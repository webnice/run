// Package run
package run

import (
	"context"
	"io"
	"os"
)

// Копирование среза байт в новый срез той же длинны.
func (run *impl) bytesCopySlice(b []byte) (n int, ret []byte) {
	ret = make([]byte, len(b))
	n = copy(ret, b)
	return
}

// Функция выполняет задачу копирования данных из потока в канал.
func (run *impl) goReader(onBegCh chan<- struct{}, onEndCh chan<- struct{}, outputCh chan<- []byte, inputFh *os.File) {
	var (
		err error
		buf []byte
		n   int
	)

	buf = make([]byte, run.bufLen)
	chanSendSignal(onBegCh)
	for {
		n, err = inputFh.Read(buf)
		j, tmp := run.bytesCopySlice(buf[:n])
		chanSendData(outputCh, tmp[:j])
		if n == 0 && err == io.EOF {
			break
		}
	}
	_ = inputFh.Close()
	chanClose(outputCh)
	chanSendSignal(onEndCh)
}

// Функция выполняет задачу копирования данных из канала в поток.
func (run *impl) goWriter(onBegCh chan<- struct{}, onEndCh chan<- struct{}, outputFh *os.File, inputCh <-chan []byte) {
	var (
		err  error
		buf  []byte
		n, k int
	)

	chanSendSignal(onBegCh)
	for buf = range inputCh {
		j, tmp := run.bytesCopySlice(buf)
		for n = 0; len(tmp[n:j]) > 0; {
			if k, err = outputFh.Write(tmp[n:j]); err != nil {
				break
			}
			n += k
		}
	}
	_ = outputFh.Close()
	chanSendSignal(onEndCh)
}

// Функция выполняет задачу ожидания завершения запущенного процесса и закрытие всех каналов и потоков данных.
func (run *impl) goProcessWait(onBegCh chan<- struct{}, cancelFn context.CancelFunc) {
	var err error

	chanSendSignal(onBegCh)
	run.processSync.Lock()
	run.debug("процесс PID: %d запушен", run.process.Pid)
	// Ожидание завершения запущенного процесса.
	if run.processStatus, err = run.process.Wait(); run.err == nil && err != nil {
		run.err = err
	}
	run.debug("процесс PID: %d завершён", run.process.Pid)
	// Отправка сигнала завершения в горутину обработки данных.
	cancelFn()
	run.process = nil
	// Закрытие канала и файловых дескрипторов, это вызовет завершение горутин.
	run.debug("закрытие каналов обмена данными")
	chanClose(run.stdinpCh)
	_ = run.pipeInpReader.Close() // STDIN
	_ = run.pipeOutWriter.Close() // STDOUT
	_ = run.pipeErrWriter.Close() // STDERR
	// Закрытие каналов передаваемых вовне.
	if run.externalOutCh != nil {
		run.debug("закрыт внешний канал STDOUT")
		chanClose(run.externalOutCh)
		run.externalOutCh = nil
	}
	if run.externalErrCh != nil {
		run.debug("закрыт внешний канал STDERR")
		chanClose(run.externalErrCh)
		run.externalErrCh = nil
	}
	// Ожидание завершения горутин.
	run.debug("завершение вспомогательных горутин, начато")
	<-run.doneInp
	<-run.doneOut
	<-run.doneErr
	<-run.doneData
	run.debug("завершение вспомогательных горутин, окончено")
	// Снятие блокировок.
	run.processWait.Done()
	run.processSync.Unlock()
}

// Функция выполняет задачу:
// 1. Передача данных между буферами;
// 2. Передача данных между каналами;
// 3. Обработка события прерывания через контекст;
func (run *impl) goProcessData(onBegCh chan<- struct{}, onEndCh chan<- struct{}, ctx context.Context) {
	var (
		err  error
		end  bool
		buf  []byte
		ext  []byte
		n, k int
	)

	run.debug("поток обмена данных запущен")
	buf, ext = make([]byte, run.bufLen), make([]byte, run.bufLen)
	chanSendSignal(onBegCh)
	for {
		if end {
			break
		}
		select {
		// Обработка сигнала завершения обработки данных после завершения работы процесса.
		case <-ctx.Done():
			run.debug("получен сигнал прерывания через контекст")
			if end, err = true, run.Kill(); run.err == nil && err != nil {
				run.err = err
			}
			continue
		// Событие поступление новых данных в функцию STDIN.
		case <-run.onNewData:
			run.debug("получен срез данных для передачи в STDIN")
			for {
				if run.bufInp.Len() <= 0 {
					break
				}
				n, err = run.bufInp.Read(buf)
				j, tmp := run.bytesCopySlice(buf[:n])
				run.stdinpCh <- tmp[:j]
				if err != nil {
					break
				}
			}
		// Событие поступления новых данных в канал STDOUT.
		case buf = <-run.stdOutCh:
			j, tmp := run.bytesCopySlice(buf)
			for n = 0; len(tmp[n:j]) > 0; {
				if k, err = run.bufOut.Write(tmp[n:j]); err != nil {
					break
				}
				if run.externalOutCh != nil {
					run.externalOutCh <- tmp[n:k]
				}
				n += k
			}
		// Событие поступления новых данных в канал STDERR.
		case buf = <-run.stdErrCh:
			j, tmp := run.bytesCopySlice(buf)
			for n = 0; len(tmp[n:j]) > 0; {
				if k, err = run.bufErr.Write(tmp[n:j]); err != nil {
					break
				}
				if run.externalErrCh != nil {
					run.externalErrCh <- tmp[n:k]
				}
				n += k
			}
		// Поступление новых данных для канала STDIN.
		case ext = <-run.externalInpCh:
			if len(ext) <= 0 {
				continue
			}
			run.debug("получены данные из канала для передачи в STDIN")
			for n = 0; len(ext[n:]) > 0; {
				j, tmp := run.bytesCopySlice(ext[n:])
				n += j
				run.stdinpCh <- tmp[:j]
			}
		}
	}
	chanSendSignal(onEndCh)
	run.debug("поток обмена данных завершён")
}

// Закрытие канала с защитой от паники.
func chanClose[T chan []byte | chan<- []byte | chan struct{}](c T) {
	defer func() { _ = recover() }()
	close(c)
}

// Отправка сигнала в канал с защитой от паники.
func chanSendSignal(c chan<- struct{}) {
	defer func() { _ = recover() }()
	c <- struct{}{}
}

// Отправка данных в канал с защитой от паники.
func chanSendData(c chan<- []byte, d []byte) {
	defer func() { _ = recover() }()
	c <- d
}
