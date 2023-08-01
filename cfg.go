// Package run
package run

import (
	"fmt"
	"os/user"
	"runtime"
	"strconv"
	"syscall"
)

// WorkingDirectory Назначение директории выполнения приложения. По умолчанию - текущая директория.
func (run *impl) WorkingDirectory(dir string) Interface {
	run.attributes.Dir = dir
	run.debug("директория выполнения процесса: %q", run.attributes.Dir)
	return run
}

// Environment Переменные окружения, устанавливаемые для приложения.
// Переменные указываются как "КЛЮЧ=Значение".
func (run *impl) Environment(env ...string) Interface {
	run.attributes.Env = make([]string, 0, len(env))
	run.attributes.Env = append(run.attributes.Env, env...)
	run.debug("переменные окружения: %v", run.attributes.Env)

	return run
}

// Chroot Запускаемое приложение выполняется в режиме chroot в указанной директории.
func (run *impl) Chroot(dir string) Interface {
	if run.attributes.Sys == nil {
		run.attributes.Sys = new(syscall.SysProcAttr)
	}
	run.attributes.Sys.Chroot = dir
	run.debug("директория chroot выполнения процесса: %q", run.attributes.Sys.Chroot)

	return run
}

// Sudo Запускаемый процесс будет запущен от указанного пользователя и группы.
// userID      - Идентификатор пользователя.
// groupID     - Идентификатор группы.
// noSetGroups - Флаг, указывающий не устанавливать дополнительные группы.
// groups      - Массив идентификаторов дополнительных групп.
func (run *impl) Sudo(userID uint32, groupID uint32, noSetGroups bool, groups ...uint32) Interface {
	if run.attributes.Sys == nil {
		run.attributes.Sys = new(syscall.SysProcAttr)
	}
	if run.attributes.Sys.Credential == nil {
		run.attributes.Sys.Credential = new(syscall.Credential)
	}
	run.attributes.Sys.Credential.Uid, run.attributes.Sys.Credential.Gid = userID, groupID
	run.attributes.Sys.Credential.NoSetGroups = noSetGroups
	run.attributes.Sys.Credential.Groups = make([]uint32, 0, len(groups))
	run.attributes.Sys.Credential.Groups = append(run.attributes.Sys.Credential.Groups, groups...)
	run.debug(
		"выполнение процесса от пользователя %d и группы %d",
		run.attributes.Sys.Credential.Uid,
		run.attributes.Sys.Credential.Gid,
	)

	return run
}

// UserID Поиск идентификатора пользователя по названию пользователя.
func (run *impl) UserID(userName string) (ret uint32, err error) {
	var (
		u *user.User
		i uint64
	)

	if u, err = user.Lookup(userName); err != nil {
		err = fmt.Errorf("поиск пользователя по имени %q прерван ошибкой: %s", userName, err)
		return
	}
	switch runtime.GOOS {
	case "windows", "plan9":
		err = fmt.Errorf("идентификатор пользователя не является числом")
	default:
		if i, err = strconv.ParseUint(u.Uid, 10, 32); err != nil {
			err = fmt.Errorf("конвертация строки %q в число прервана ошибкой: %s", u.Uid, err)
			return
		}
		ret = uint32(i)
	}

	return
}

// GroupID Поиск идентификатора группы пользователя по названию группы.
func (run *impl) GroupID(groupName string) (ret uint32, err error) {
	var (
		g *user.Group
		i uint64
	)

	if g, err = user.LookupGroup(groupName); err != nil {
		err = fmt.Errorf("поиск группы по имени %q прерван ошибкой: %s", groupName, err)
		return
	}
	switch runtime.GOOS {
	case "windows", "plan9":
		err = fmt.Errorf("идентификатор группы не является числом")
	default:
		if i, err = strconv.ParseUint(g.Gid, 10, 32); err != nil {
			err = fmt.Errorf("конвертация строки %q в число прервана ошибкой: %s", g.Gid, err)
			return
		}
		ret = uint32(i)
	}

	return
}

// Command Функция возвращает текущую запущенную команду.
func (run *impl) Command() (ret []string) { return run.cmd }
