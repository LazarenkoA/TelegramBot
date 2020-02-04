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
	logrus.WithField("Каталог", g.RepDir).Debug("GIT. checkout")

	cmd := exec.Command("git", "checkout", branch)
	if _, err := g.run(cmd, g.RepDir); err != nil {
		return err // Странно, но почему-то гит информацию о том что изменилась ветка пишет в Stderr
	}
	return nil
}

func (g *Git) Pull(branch string) (err error) {
	logrus.WithField("Каталог", g.RepDir).Debug("GIT. Pull")

	// Если pull не проходит из-за мержа, то это поможет:
	// git fetch --all
	// git reset --hard origin/master
	// git pull origin/master

	if _, err = os.Stat(g.RepDir); os.IsNotExist(err) {
		err = fmt.Errorf("каталог %q Git репозитория не найден", g.RepDir)
		logrus.WithField("Каталог", g.RepDir).Error(err)
	}

	if err = g.checkout(branch); err != nil {
		return
	}

	cmd := exec.Command("git", "pull")
	if _, err = g.run(cmd, g.RepDir); err != nil {
		return err
	}
	return nil
}

func (g *Git) GetBranches() (result []string, err error) {
	if _, err = os.Stat(g.RepDir); os.IsNotExist(err) {
		err = fmt.Errorf("каталог %q Git репозитория не найден", g.RepDir)
		logrus.WithField("Каталог", g.RepDir).Error(err)
	}
	result = []string{}

	cmd := exec.Command("git", "branch")
	if res, err := g.run(cmd, g.RepDir); err != nil {
		return []string{}, err
	} else {
		for _, branch := range strings.Split(res, "\n") {
			if branch == "" {
				continue
			}
			result = append(result, strings.Trim(branch, " *"))
		}
	}
	return result, nil
}

func (g *Git) BranchExist(branchName string) bool {
	if branches, err := g.GetBranches(); err != nil {
		logrus.WithField("Branch", branchName).Errorf("Произошла ошибка при получении списка веток: %v", err)
		return false
	} else {
		for _, branch := range branches {
			if strings.ToLower(branchName) == strings.ToLower(branch) {
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
		err = fmt.Errorf("файл %q не найден", file)
		logrus.WithField("Файл", file).Error(err)
	}

	if err = g.checkout(branch); err != nil {
		return fmt.Errorf("Ошибка переключение на ветку %q: %v", branch, err) // не смогли переключить ветку, выходим
	}
	g.Pull(branch)

	param := []string{}
	param = append(param, "commit")
	param = append(param, fmt.Sprintf("--cleanup=verbatim"))
	param = append(param, fmt.Sprintf("-m %q", commit))
	param = append(param, strings.Replace(file, "\\", "/", -1))

	cmdCommit := exec.Command("git", param...)
	if _, err = g.run(cmdCommit, dir); err == nil {
		g.Push()
		g.optimization()
	}

	return err
}

func (g *Git) Push() (err error) {
	logrus.WithField("Каталог", g.RepDir).Debug("GIT. Push")
	if _, err = os.Stat(g.RepDir); os.IsNotExist(err) {
		err = fmt.Errorf("каталог %q Git репозитория не найден", g.RepDir)
		logrus.WithField("Каталог", g.RepDir).Error(err)
	}

	cmd := exec.Command("git", "push")
	if _, err := g.run(cmd, g.RepDir); err != nil {
		return err
	}
	return nil
}

func (g *Git) optimization() (err error) {
	logrus.Debug("GIT. optimization")

	if _, err = os.Stat(g.RepDir); os.IsNotExist(err) {
		err = fmt.Errorf("каталог %q Git репозитория не найден", g.RepDir)
		logrus.WithField("Каталог", g.RepDir).Error(err)
	}

	cmd := exec.Command("git", "gc", "--auto")
	if _, err := g.run(cmd, g.RepDir); err != nil {
		return err
	}
	return nil
}

func (g *Git) ResetHard(branch string) (err error) {
	logrus.WithField("branch", branch).Debug("GIT. ResetHard")
	defer func() {
		if e := recover(); e == nil && err == nil {
			logrus.Debug("GIT. ResetHard - Success")
		} else {
			logrus.Debug("GIT. ResetHard - Fail")
		}
	}()

	if _, err = os.Stat(g.RepDir); os.IsNotExist(err) {
		err = fmt.Errorf("Каталог %q Git репозитория не найден", g.RepDir)
		logrus.WithField("Каталог", g.RepDir).Error(err)
	}

	if branch != "" {
		g.checkout(branch)
	}
	logrus.WithField("branch", branch).Debug("GIT. fetch")

	cmd := exec.Command("git", "fetch", "origin")
	g.run(cmd, g.RepDir)

	cmd = exec.Command("git", "reset", "--hard", "origin/"+branch)
	if _, err = g.run(cmd, g.RepDir); err != nil {
		return err
	} else {
		return nil
	}
}

func (g *Git) run(cmd *exec.Cmd, dir string) (string, error) {
	logrus.WithField("Исполняемый файл", cmd.Path).
		WithField("Параметры", cmd.Args).
		WithField("Каталог", dir).
		Debug("Выполняется команда git")

	cmd.Dir = dir
	cmd.Stdout = new(bytes.Buffer)
	cmd.Stderr = new(bytes.Buffer)

	err := cmd.Run()
	stderr := cmd.Stderr.(*bytes.Buffer).String()
	stdout := cmd.Stdout.(*bytes.Buffer).String()
	if err != nil {
		errText := fmt.Sprintf("Произошла ошибка запуска:\n err:%v \n", err.Error())
		if stderr != "" {
			errText += fmt.Sprintf("StdErr:%v \n", stderr)
		}
		if stdout != "" {
			errText += fmt.Sprintf("Stdout:%v \n", stdout)
		}
		logrus.WithField("Исполняемый файл", cmd.Path).Error(errText)
		return "", fmt.Errorf(errText)
	}

	return stdout, err
}
