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
	const errReaderClose = "закрытие канала чтения данных прервано ошибкой: %s"
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
	if err = inputFh.Close(); err != nil {
		run.debug(errReaderClose, err)
	}
	chanClose(outputCh)
	chanSendSignal(onEndCh)
}

// Функция выполняет задачу копирования данных из канала в поток.
func (run *impl) goWriter(onBegCh chan<- struct{}, onEndCh chan<- struct{}, outputFh *os.File, inputCh <-chan []byte) {
	const (
		errWriter = "запись данных в исходящий поток прервана ошибкой: %s"
		errClose  = "закрытие потока записи данных прервана ошибкой: %s"
	)
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
				run.debug(errWriter, err)
				break
			}
			n += k
		}
	}
	if err = outputFh.Close(); err != nil {
		run.debug(errClose, err)
	}
	chanSendSignal(onEndCh)
}

// Функция выполняет задачу ожидания завершения запущенного процесса и закрытие всех каналов и потоков данных.
func (run *impl) goProcessWait(onBegCh chan<- struct{}, cancelFn context.CancelFunc) {
	const (
		msgPidBeg    = "процесс PID: %d запушен"
		msgPidEnd    = "процесс PID: %d завершён"
		mcgCloseChan = "закрытие каналов обмена данными"
		msgCloseOut  = "закрыт внешний канал STDOUT"
		msgCloseErr  = "закрыт внешний канал STDERR"
		msgStopBeg   = "завершение вспомогательных горутин, начато"
		msgStopEnd   = "завершение вспомогательных горутин, окончено"
		errCloseInp  = "закрытие трубы STDIN прервано ошибкой: %s"
		errCloseOut  = "закрытие трубы STDOUT прервано ошибкой: %s"
		errCloseErr  = "закрытие трубы STDERR прервано ошибкой: %s"
	)
	var err error

	chanSendSignal(onBegCh)
	run.processSync.Lock()
	run.debug(msgPidBeg, run.process.Pid)
	// Ожидание завершения запущенного процесса.
	if run.processStatus, err = run.process.Wait(); run.err == nil && err != nil {
		run.err = err
	}
	run.debug(msgPidEnd, run.process.Pid)
	// Отправка сигнала завершения в горутину обработки данных.
	cancelFn()
	run.process = nil
	// Закрытие канала и файловых дескрипторов, это вызовет завершение горутин.
	run.debug(mcgCloseChan)
	chanClose(run.stdinpCh)
	if err = run.pipeInpReader.Close(); err != nil { // STDIN
		run.debug(errCloseInp, err)
	}
	if err = run.pipeOutWriter.Close(); err != nil { // STDOUT
		run.debug(errCloseOut, err)
	}
	if err = run.pipeErrWriter.Close(); err != nil { // STDERR
		run.debug(errCloseErr, err)
	}
	// Закрытие каналов передаваемых вовне.
	if run.externalOutCh != nil {
		run.debug(msgCloseOut)
		chanClose(run.externalOutCh)
		run.externalOutCh = nil
	}
	if run.externalErrCh != nil {
		run.debug(msgCloseErr)
		chanClose(run.externalErrCh)
		run.externalErrCh = nil
	}
	// Ожидание завершения горутин.
	run.debug(msgStopBeg)
	<-run.doneInp
	<-run.doneOut
	<-run.doneErr
	<-run.doneData
	run.debug(msgStopEnd)
	// Снятие блокировок.
	run.processWait.Done()
	run.processSync.Unlock()
}

// Функция выполняет задачу:
// 1. Передача данных между буферами;
// 2. Передача данных между каналами;
// 3. Обработка события прерывания через контекст;
func (run *impl) goProcessData(onBegCh chan<- struct{}, onEndCh chan<- struct{}, ctx context.Context) {
	const (
		msgProcBeg  = "поток обмена данных запущен"
		msgProcEnd  = "поток обмена данных завершён"
		msgCancel   = "получен сигнал прерывания через контекст"
		msgToStdInp = "получен срез данных для передачи в STDIN"
		msgFrStdInp = "получены данные из канала для передачи в STDIN"
	)
	var (
		err  error
		end  bool
		buf  []byte
		ext  []byte
		n, k int
	)

	run.debug(msgProcBeg)
	buf, ext = make([]byte, run.bufLen), make([]byte, run.bufLen)
	chanSendSignal(onBegCh)
	for {
		if end {
			break
		}
		select {
		// Обработка сигнала завершения обработки данных после завершения работы процесса.
		case <-ctx.Done():
			run.debug(msgCancel)
			if end = true; run.process != nil {
				if err = run.Kill(); run.err == nil && err != nil {
					run.err = err
				}
			}
			continue
		// Событие поступление новых данных в функцию STDIN.
		case <-run.onNewData:
			run.debug(msgToStdInp)
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
			run.debug(msgFrStdInp)
			for n = 0; len(ext[n:]) > 0; {
				j, tmp := run.bytesCopySlice(ext[n:])
				n += j
				run.stdinpCh <- tmp[:j]
			}
		}
	}
	chanSendSignal(onEndCh)
	run.debug(msgProcEnd)
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
