package run

import (
	"bytes"
	"context"
	"os"
	"sync"
)

const (
	bufLength  = 4 * 1024
	chanLength = 1000
)

// Объект сущности пакета.
type impl struct {
	debugMode     bool             // Режим отладки.
	bufLen        int              // Размер буфера среза байт.
	chanLen       int              // Размер буфера канала.
	err           error            // Последняя возникшая ошибка препятствующая работе пакета.
	cmd           []string         // Команда.
	pipeInpReader *os.File         // STDIN - труба чтения, связанная с трубой записи.
	pipeInpWriter *os.File         // STDIN - труба записи, связанная с трубой чтения.
	pipeOutReader *os.File         // STDOUT - труба чтения, связанная с трубой записи.
	pipeOutWriter *os.File         // STDOUT - труба записи, связанная с трубой чтения.
	pipeErrReader *os.File         // STDERR - труба чтения, связанная с трубой записи.
	pipeErrWriter *os.File         // STDERR - труба записи, связанная с трубой чтения.
	attributes    *os.ProcAttr     // Атрибуты запуска.
	context       context.Context  // Контекст.
	processSync   *sync.Mutex      // Контроль монопольного доступа к process.
	process       *os.Process      // Описание запущенного процесса.
	processStatus *os.ProcessState // Статус завершения процесса.
	processWait   *sync.WaitGroup  // Блокировка на время выполнения процесса.
	doneInp       chan struct{}    // Канал передачи сигнала о завершении вспомогательной горутины STDIN.
	doneOut       chan struct{}    // Канал передачи сигнала о завершении вспомогательной горутины STDOUT.
	doneErr       chan struct{}    // Канал передачи сигнала о завершении вспомогательной горутины STDERR.
	doneData      chan struct{}    // Канал передачи сигнала о завершении вспомогательной горутины обработки данных.
	stdinpCh      chan []byte      // Канал STDIN.
	stdOutCh      chan []byte      // Канал STDOUT.
	stdErrCh      chan []byte      // Канал STDERR.
	onNewData     chan struct{}    // Буферизированный канал для обработки событие поступления новых данных.
	bufInp        *bytes.Buffer    // Данные отправляемые в STDIN после запуска приложения.
	bufOut        *bytes.Buffer    // Данные полученные из потока STDOUT.
	bufErr        *bytes.Buffer    // Данные полученные из потока STDERR.
	externalInpCh <-chan []byte    // Канал, полученный извне, с данными для STDIN.
	externalOutCh chan []byte      // Канал, передаваемый вовне, с данными из STDOUT.
	externalErrCh chan []byte      // Канал, передаваемый вовне, с данными из STDERR.
}
