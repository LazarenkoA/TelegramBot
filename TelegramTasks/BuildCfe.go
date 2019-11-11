package telegram

import (
	cf "1C/Configuration"
	git "1C/Git"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

type EventBuildCfe struct {
	BeforeBuild   []func()
	AfterBuild    []func(ext cf.IConfiguration)
	AfterAllBuild []func() // Событие которое вызывается при сборе всех расширений
}

type BuildCfe struct {
	BaseTask
	EventBuildCfe

	ChoseExtName  string
	HideAllButtun bool
	Ext           *cf.ConfCommonData
	Branch        string
	//notInvokeInnerFinish bool
}

func (B *BuildCfe) ChoseExt(ChoseData string) {
	B.ChoseExtName = ChoseData
	B.next("")
}

func (B *BuildCfe) ChoseAll() {
	B.ChoseExtName = ""
	B.next("")
}

func (B *BuildCfe) ChoseBranch(Branch string) {
	B.Branch = Branch
	if B.Branch == "" {
		B.next("")
		go B.Invoke()
		return
	}

	g := new(git.Git)
	g.RepDir = Confs.GitRep

	if err := g.Pull(B.Branch); err != nil {
		B.baseFinishMsg("Произошла ошибка при получении данных из Git: " + err.Error())
		return
	}

	B.next(fmt.Sprintf("Данные обновлены из Git (ветка %q).\nНачинаю собирать расширения.", B.Branch))
	go B.Invoke()
}

func (B *BuildCfe) Invoke() {
	sendError := func(Msg string) {
		logrus.WithField("Каталог сохранения расширений", B.Ext.OutDir).Error(Msg)
		B.baseFinishMsg(Msg)
	}

	defer func() {
		if err := recover(); err != nil {
			sendError(fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err))
		} else {
			// вызываем события
			for _, f := range B.AfterAllBuild {
				f()
			}
		}
		B.outFinish()
	}()

	wg := new(sync.WaitGroup)
	wgError := new(sync.WaitGroup)
	chResult := make(chan cf.IConfiguration, pool)
	chError := make(chan error, pool)

	for i := 0; i < pool; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for c := range chResult {
				// вызываем события
				for _, f := range B.AfterBuild {
					f(c)
				}
			}
		}()
	}

	// вызываем события
	for _, f := range B.BeforeBuild {
		f()
	}

	// обязательно в отдельной горутине, или размер канала chError делать = кол-ву расширений
	wgError.Add(1)
	go func() {
		defer wgError.Done()
		for err := range chError {
			sendError(fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err))
		}
	}()

	if err := B.Ext.BuildExtensions(chResult, chError, B.ChoseExtName); err != nil {
		logrus.Panic(err) // в defer перехват
	}

	wg.Wait()
	wgError.Wait()
}

func (B *BuildCfe) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	B.BaseTask.Initialise(bot, update, finish)

	B.Ext = new(cf.ConfCommonData).New(Confs)
	firstStep := new(step).Construct("Выберите расширения", "BuildCfe-1", B, ButtonCancel|ButtonBack, 2)
	for _, ext := range B.Ext.GetExtensions() {
		name := ext.GetName()
		firstStep.appendButton(name, func() { B.ChoseExt(name) })
	}
	if !B.HideAllButtun {
		firstStep.appendButton("Все", B.ChoseAll).reverseButton()
	}

	if Confs.GitRep == "" {
		logrus.Panic("В настройках не задан GIT репозиторий")
	}
	gitStep := new(step).Construct("Выберите Git ветку для обновления", "BuildCfe-2", B, ButtonCancel|ButtonBack, 2)

	g := new(git.Git)
	g.RepDir = Confs.GitRep
	if list, err := g.GetBranches(); err == nil {
		for _, Branch := range list {
			var BranchName string = Branch
			gitStep.appendButton(Branch, func() { B.ChoseBranch(BranchName) })
		}
	} else {
		B.baseFinishMsg("Произошла ошибка при получении Git веток: " + err.Error())
		return B
	}
	gitStep.appendButton("Не обновлять", func() { B.ChoseBranch("") })

	B.steps = []IStep{
		firstStep,
		gitStep,
		new(step).Construct("⚙️ Начинаю собирать расширения.", "BuildCfe-3", B, 0, 2),
	}

	B.AfterBuild = append(B.AfterBuild, func(ext cf.IConfiguration) {
		_, fileName := filepath.Split(ext.GetFile())

		msg := tgbotapi.NewMessage(B.ChatID, fmt.Sprintf("Собрано расширение %q", fileName))
		B.bot.Send(msg)
	})
	B.AfterAllBuild = append(B.AfterAllBuild, B.innerFinish)

	B.AppendDescription(B.name)
	return B
}

func (B *BuildCfe) Start() {
	logrus.WithField("description", B.GetDescription()).Debug("Start")

	B.steps[B.currentStep].invoke(&B.BaseTask)
}

func (B *BuildCfe) InfoWrapper(task ITask) {
	OutDir := Confs.OutDir
	if strings.Trim(OutDir, "") == "" {
		OutDir, _ = ioutil.TempDir("", "")
	}
	B.info = fmt.Sprintf("ℹ Команда выгружает файл расширений (*.cfe), файл сохраняется на диске в каталог %v.", OutDir)
	B.BaseTask.InfoWrapper(task)
}

func (B *BuildCfe) innerFinish() {
	Msg := fmt.Sprintf("Расширения собраны и ожидают вас в каталоге %v", B.Ext.OutDir)
	B.baseFinishMsg(Msg)
}
