package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"
)

type Git struct {
	RepDir string
	//gitBin string // если стоит git, то в системной переменной path будет путь к git
}

func (g *Git) checkout(branch string) error {
	logrus.WithField("Каталог", g.RepDir).Debug("checkout")

	cmd := exec.Command("git", "checkout", branch)
	if err, _ := g.run(cmd, g.RepDir); err != nil {
		return err // Странно, но почему-то гит информацию о том что изменилась ветка пишет в Stderr
	} else {
		return nil
	}
}

func (g *Git) Pull(branch string) (err error) {
	logrus.WithField("Каталог", g.RepDir).Debug("Pull")

	if _, err = os.Stat(g.RepDir); os.IsNotExist(err) {
		err = fmt.Errorf("Каталог %q Git репозитория не найден", g.RepDir)
		logrus.WithField("Каталог", g.RepDir).Error(err)
	}

	g.checkout(branch)

	cmd := exec.Command("git", "pull")
	if err, _ := g.run(cmd, g.RepDir); err != nil {
		return err
	} else {
		return nil
	}
}

func (g *Git) GetBranches() (err error, result []string) {
	if _, err = os.Stat(g.RepDir); os.IsNotExist(err) {
		err = fmt.Errorf("Каталог %q Git репозитория не найден", g.RepDir)
		logrus.WithField("Каталог", g.RepDir).Error(err)
	}
	result = []string{}

	cmd := exec.Command("git", "branch")
	if err, res := g.run(cmd, g.RepDir); err != nil {
		return err, []string{}
	} else {
		for _, branch := range strings.Split(res, "\n") {
			if branch == "" {
				continue
			}
			result = append(result, strings.Trim(branch, " *"))
		}
		return nil, result
	}
}

func (g *Git) CommitAndPush() (err error) {

	return nil
}

func (g *Git) run(cmd *exec.Cmd, dir string) (error, string) {
	logrus.WithField("Исполняемый файл", cmd.Path).
		WithField("Параметры", cmd.Args).
		WithField("Каталог", dir).
		Debug("Выполняется команда git")

	cmd.Dir = dir
	cmd.Stdout = new(bytes.Buffer)
	cmd.Stderr = new(bytes.Buffer)

	err := cmd.Run()
	stderr := string(cmd.Stderr.(*bytes.Buffer).Bytes())
	if err != nil {
		errText := fmt.Sprintf("Произошла ошибка запуска:\n err:%q \n", string(err.Error()))
		if stderr != "" {
			errText += fmt.Sprintf("StdErr:%q \n", stderr)
		}
		logrus.Error(errText)
		return fmt.Errorf(errText), ""
	}

	return nil, string(cmd.Stdout.(*bytes.Buffer).Bytes())
}
