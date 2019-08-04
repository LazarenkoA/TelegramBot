package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

func (g *Git) BranchExist(BranchName string) bool {
	if err, branches := g.GetBranches(); err != nil {
		logrus.WithField("Branch", BranchName).Errorf("Произошла ошибка при получении списка веток: %v", err)
		return false
	} else {
		for _, branch := range branches {
			if strings.ToLower(BranchName) == strings.ToLower(branch) {
				return true
			}
		}
	}

	return false
}

func (g *Git) CommitAndPush(branch, file, commit string) (err error) {
	logrus.WithField("Файл", file).Debug("CommitAndPush")

	dir, _ := filepath.Split(file)

	if _, err = os.Stat(file); os.IsNotExist(err) {
		err = fmt.Errorf("Файл %q не найден", file)
		logrus.WithField("Файл", file).Error(err)
	}

	g.checkout(branch)
	g.Pull(branch)

	param := []string{}
	param = append(param, "commit")
	param = append(param, fmt.Sprintf("--cleanup=verbatim"))
	param = append(param, fmt.Sprintf("-m %q", commit))
	param = append(param, strings.Replace(file, "\\", "/", -1))

	cmdCommit := exec.Command("git", param...)
	if err, _ = g.run(cmdCommit, dir); err == nil {
		g.Push()
		g.optimization()
	}

	return err
}

func (g *Git) Push() (err error) {
	logrus.WithField("Каталог", g.RepDir).Debug("Push")
	if _, err = os.Stat(g.RepDir); os.IsNotExist(err) {
		err = fmt.Errorf("Каталог %q Git репозитория не найден", g.RepDir)
		logrus.WithField("Каталог", g.RepDir).Error(err)
	}

	cmd := exec.Command("git", "push")
	if err, _ := g.run(cmd, g.RepDir); err != nil {
		return err
	} else {
		return nil
	}
}

func (g *Git) optimization() (err error) {
	logrus.Debug("optimization")

	if _, err = os.Stat(g.RepDir); os.IsNotExist(err) {
		err = fmt.Errorf("Каталог %q Git репозитория не найден", g.RepDir)
		logrus.WithField("Каталог", g.RepDir).Error(err)
	}

	cmd := exec.Command("git", "gc", "--auto")
	if err, _ := g.run(cmd, g.RepDir); err != nil {
		return err
	} else {
		return nil
	}
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
	stderr := cmd.Stderr.(*bytes.Buffer).String()
	if err != nil {
		errText := fmt.Sprintf("Произошла ошибка запуска:\n err:%q \n", string(err.Error()))
		if stderr != "" {
			errText += fmt.Sprintf("StdErr:%q \n", stderr)
		}
		logrus.Error(errText)
		return fmt.Errorf(errText), ""
	}

	return nil, cmd.Stdout.(*bytes.Buffer).String()
}
