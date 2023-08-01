// Package run
package run

// StdInCh Канал с данными для потока STDIN. Канал должен быть закрыт там же где открывался.
// Функция читает канал и передаёт процессу данные, до тех пор пока канал открыт и процесс запущен.
func (run *impl) StdInCh(ch <-chan []byte) Interface { run.externalInpCh = ch; return run }

// StdIn Данные, отправляемые процессу в поток STDIN после запуска процесса.
func (run *impl) StdIn(buf []byte) Interface {
	_, _ = run.bufInp.Write(buf)
	chanSendSignal(run.onNewData)
	return run
}

// StdOutCh Канал с данными полученными из процесса через поток STDOUT. Канал будет закрыт после завершения
// процесса. Канал не создаётся, если функция не вызывалась.
func (run *impl) StdOutCh() (ret <-chan []byte) {
	run.debug("открыт внешний канал STDOUT")
	run.externalOutCh = make(chan []byte, run.chanLen)
	return run.externalOutCh
}

// StdOut Данные, полученные от процесса через поток STDOUT.
func (run *impl) StdOut() (ret []byte) { return run.bufOut.Bytes() }

// StdErrCh Канал с данными полученными из процесса через поток STDERR. Канал будет закрыт после завершения
// процесса. Канал не создаётся, если функция не вызывалась.
func (run *impl) StdErrCh() (ret <-chan []byte) {
	run.debug("открыт внешний канал STDERR")
	run.externalErrCh = make(chan []byte, run.chanLen)
	return run.externalErrCh
}

// StdErr Данные, полученные от процесса через поток STDERR.
func (run *impl) StdErr() (ret []byte) { return run.bufErr.Bytes() }
