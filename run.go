// Package run
package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// New Конструктор объекта сущности пакета, возвращается интерфейс пакета.
func New() Interface {
	var run = &impl{
		bufLen:  bufLength,
		chanLen: chanLength,
		bufInp:  &bytes.Buffer{},
		bufOut:  &bytes.Buffer{},
		bufErr:  &bytes.Buffer{},
	}
	run.err = run.init()
	return run
}

func (run *impl) debug(format string, args ...any) {
	if !run.debugMode {
		return
	}
	println(fmt.Sprintf(format, args...))
}

// Инициализатор объекта пакета.
// Функция вызывается так же при сбросе данных пакета, для переиспользования.
func (run *impl) init() (err error) {
	run.debug("инициализация пакета, начато")
	run.err = nil
	run.cmd = run.cmd[:0]
	run.context = nil
	run.processSync = new(sync.Mutex)
	run.process = nil
	run.processStatus = nil
	run.processWait = new(sync.WaitGroup)
	// Каналы взаимодействия с потоками.
	chanClose(run.stdinpCh)
	run.stdinpCh = make(chan []byte, run.chanLen)
	chanClose(run.stdOutCh)
	run.stdOutCh = make(chan []byte, run.chanLen)
	chanClose(run.stdErrCh)
	run.stdErrCh = make(chan []byte, run.chanLen)
	// Канал передачи сигнала о завершении вспомогательной горутины обработки данных.
	chanClose(run.doneData)
	run.doneData = make(chan struct{})
	// Каналы передачи сигнала о завершении вспомогательной горутины
	chanClose(run.doneInp)
	run.doneInp = make(chan struct{})
	chanClose(run.doneOut)
	run.doneOut = make(chan struct{})
	chanClose(run.doneErr)
	run.doneErr = make(chan struct{})
	// Буферизированный канал для обработки событие поступления новых данных в STDIN.
	chanClose(run.onNewData)
	run.onNewData = make(chan struct{}, run.chanLen)
	run.bufInp.Reset()
	run.bufOut.Reset()
	run.bufErr.Reset()
	// Каналы обмена данными потоков с внешними источниками и получателями.
	run.externalInpCh = nil
	chanClose(run.externalOutCh)
	run.externalOutCh = nil
	chanClose(run.externalErrCh)
	run.externalErrCh = nil
	// Потоки взаимодействия с запускаемым приложением.
	if run.pipeInpReader, run.pipeInpWriter, err = os.Pipe(); err != nil {
		err = fmt.Errorf("создание трубы для STDIN прервано ошибкой: %s", err)
		return
	}
	if run.pipeOutReader, run.pipeOutWriter, err = os.Pipe(); err != nil {
		err = fmt.Errorf("создание трубы для STDOUT прервано ошибкой: %s", err)
		return
	}
	if run.pipeErrReader, run.pipeErrWriter, err = os.Pipe(); err != nil {
		err = fmt.Errorf("создание трубы для STDERR прервано ошибкой: %s", err)
		return
	}
	run.attributes = &os.ProcAttr{}
	run.attributes.Files = make([]*os.File, 0, 3)
	run.attributes.Files = append(run.attributes.Files, run.pipeInpReader) // STDIN
	run.attributes.Files = append(run.attributes.Files, run.pipeOutWriter) // STDOUT
	run.attributes.Files = append(run.attributes.Files, run.pipeErrWriter) // STDERR
	run.debug("инициализация пакета, завершено")

	return
}

// Debug Установка режима отладки.
func (run *impl) Debug(isDebug bool) Interface { run.debugMode = isDebug; return run }

// Error Ошибка, возникшая в функции не возвращающей ошибки.
func (run *impl) Error() error { return run.err }

// Run Запуск приложения и возвращение из функции без ожидания завершения приложения.
// Если передан контекст не равный nil, тогда прерывание через контекст завершает работу приложения аналогично
// вызову функции Kill().
func (run *impl) Run(ctx context.Context, args ...string) Interface {
	var (
		proc           string
		doneBeg        chan struct{}
		processContext context.Context    // Контекст завершения вспомогательной горутины обработки данных.
		processCancel  context.CancelFunc // Функция завершения вспомогательной горутины обработки данных.
	)

	run.processSync.Lock()
	defer run.processSync.Unlock()
	if run.process != nil {
		run.err = fmt.Errorf(
			"процесс уже запущен, либо пакет используется, выполните Reset() передповторным использованием",
		)
		return run
	}
	run.processStatus = nil
	// Если была ошибка в процессе инициализации, возвращаем её сейчас.
	if run.err != nil {
		return run
	}
	run.context = ctx
	if run.context != nil {
		processContext, processCancel = context.WithCancel(run.context)
	} else {
		processContext, processCancel = context.WithCancel(context.Background())
	}
	// Рабочая директория.
	if run.attributes.Dir != "" {
		if _, run.err = os.Stat(run.attributes.Dir); run.err != nil {
			run.err = fmt.Errorf("указана не доступная рабочая директория %q, ошибка: %s", run.attributes.Dir, run.err)
			processCancel()
			return run
		}
	}
	// Проверка запускаемой программы.
	if len(args) == 0 {
		run.err = errors.New("не указана программа для запуска")
		processCancel()
		return run
	}
	if proc, run.err = exec.LookPath(args[0]); run.err != nil {
		processCancel()
		run.err = fmt.Errorf("поиск программы %q прерван ошибкой: %s", args[0], run.err)
		return run
	}
	// Запуск вспомогательных горутин с контролем того что они уже запустились и работаю.
	doneBeg = make(chan struct{})
	run.debug("запуск вспомогательных горутин, начат")
	// STDIN
	go run.goWriter(doneBeg, run.doneInp, run.pipeInpWriter, run.stdinpCh)
	<-doneBeg // Ожидание гарантированного старта горутины.
	// STDOUT
	go run.goReader(doneBeg, run.doneOut, run.stdOutCh, run.pipeOutReader)
	<-doneBeg // Ожидание гарантированного старта горутины.
	// STDERR
	go run.goReader(doneBeg, run.doneErr, run.stdErrCh, run.pipeErrReader)
	<-doneBeg // Ожидание гарантированного старта горутины.
	run.debug("запуск вспомогательных горутин, окончен")
	// Запуск процесса.
	run.cmd = make([]string, 0, len(args))
	run.cmd = append([]string{proc}, args[1:]...)
	run.debug("выполнение программы: %s", strings.Join(run.cmd, " "))
	if run.process, run.err = os.StartProcess(proc, run.cmd, run.attributes); run.err != nil {
		run.err = fmt.Errorf("выполнение программы %q прервано ошибкой: %s", proc, run.err)
		processCancel()
		return run
	}
	// Запуск вспомогательной горутины обработки данных.
	go run.goProcessData(doneBeg, run.doneData, processContext)
	<-doneBeg
	// Запуск вспомогательной горутины ожидания завершения процесса.
	run.processWait.Add(1)
	go run.goProcessWait(doneBeg, processCancel)
	<-doneBeg
	chanClose(doneBeg)

	return run
}

// RunWait Запуск приложения и ожидание завершения приложения.
// Если передан контекст не равный nil, тогда прерывание через контекст завершает работу приложения аналогично
// вызову функции Kill().
func (run *impl) RunWait(ctx context.Context, args ...string) (ret *os.ProcessState, err error) {
	if err = run.
		Run(ctx, args...).
		Error(); err != nil {
		return
	}
	if ret, err = run.Wait(); err != nil {
		return
	}

	return
}

// Wait Ожидание завершения ранее запущенного приложения.
func (run *impl) Wait() (ret *os.ProcessState, err error) {
	if run.process == nil {
		err = fmt.Errorf("процесс не запущен")
		return
	}
	run.processWait.Wait()
	if run.processStatus != nil {
		ret, err = run.processStatus, run.err
		return
	}

	return
}

// Pid Возвращает PID процесса. Если процесс не был запущен, возвращается -1.
func (run *impl) Pid() int {
	if run.process == nil {
		return -1
	}
	return run.process.Pid
}

// Signal Отправка сигнала ранее запущенному приложению.
func (run *impl) Signal(sig os.Signal) error {
	if run.process == nil {
		return errors.New("процесс не запущен")
	}
	return run.process.Signal(sig)
}

// Kill Завершение ранее запущенного приложения.
func (run *impl) Kill() error {
	if run.process == nil {
		return errors.New("процесс не запущен")
	}
	return run.process.Kill()
}

// Release Освобождение всех ресурсов запущенного приложения.
// Release необходимо выполнять только в случае если Wait() не работает.
func (run *impl) Release() error {
	if run.process == nil {
		return errors.New("процесс не запущен")
	}
	return run.process.Release()
}

// Reset Завершение приложения, если оно было запущено, сброс всех настроек и подготовка пакета для
// повторного использования.
func (run *impl) Reset() Interface {
	const tryCount = 4
	var (
		err  error
		proc *os.Process
		try  uint
		pid  int
	)

	if run.process != nil {
		pid = run.process.Pid
		if proc, err = os.FindProcess(pid); err == nil {
			run.debug("передача процессу %d сигнала SIGTERM (#16)", pid)
			for try = 0; try < tryCount && err == nil; try++ {
				err = proc.Signal(syscall.SIGTERM)
				<-time.After(time.Second)
			}
		}
	}
	if run.process != nil {
		pid = run.process.Pid
		if proc, err = os.FindProcess(pid); err == nil {
			run.debug("передача процессу %d сигнала SIGKILL (#9)", pid)
			for try = 0; try < tryCount/2 && err == nil; try++ {
				err = proc.Kill()
				<-time.After(time.Second)
			}
		}
	}
	if run.process != nil {
		pid = run.process.Pid
		run.debug("освобождение процесса %d", pid)
		_ = run.Release()
	}
	run.processSync.Lock()
	run.processSync.Unlock()
	run.debugMode = false
	run.err = run.init()

	return run
}
