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
	cmd := exec.Command("git", "checkout", branch)
	cmd.Dir = g.RepDir
	if err, _ := g.run(cmd); err != nil {
		return err // Странно, но почему-то гит информацию о том что изменилась ветка пишет в Stderr
	} else {
		return nil
	}
}

func (g *Git) Pull(branch string) error {
	if _, err := os.Stat(g.RepDir); os.IsNotExist(err) {
		logrus.WithField("Каталог", g.RepDir).Panic("Каталог Git репозитория не найден")
		return fmt.Errorf("Каталог %q Git репозитория не найден", g.RepDir)
	}

	g.checkout(branch)

	cmd := exec.Command("git", "pull")
	cmd.Dir = g.RepDir
	if err, _ := g.run(cmd); err != nil {
		return err
	} else {
		return nil
	}
}

func (g *Git) GetBranches() (error, []string) {
	if _, err := os.Stat(g.RepDir); os.IsNotExist(err) {
		logrus.WithField("Каталог", g.RepDir).Panic("Каталог Git репозитория не найден")
		return fmt.Errorf("Каталог %q Git репозитория не найден", g.RepDir), []string{}
	}
	result := []string{}

	cmd := exec.Command("git", "branch")
	cmd.Dir = g.RepDir
	if err, res := g.run(cmd); err != nil {
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

func (g *Git) run(cmd *exec.Cmd) (error, string) {
	logrus.WithField("Исполняемый файл", cmd.Path).WithField("Параметры", cmd.Args).Debug("Выполняется команда git")

	cmd.Stdout = new(bytes.Buffer)
	cmd.Stderr = new(bytes.Buffer)

	err := cmd.Run()
	stderr := string(cmd.Stderr.(*bytes.Buffer).Bytes())
	if stderr != "" {
		errText := fmt.Sprintf("Произошла ошибка запуска:\nStdErr:%q", stderr)
		logrus.Error(errText)
		return fmt.Errorf(errText), ""
	}
	if err != nil {
		errText := fmt.Sprintf("Произошла ошибка запуска:\nerr:%q", string(err.Error()))
		logrus.Error(errText)
		return fmt.Errorf(errText), ""
	}

	return nil, string(cmd.Stdout.(*bytes.Buffer).Bytes())
}
