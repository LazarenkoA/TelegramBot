package telegram

import (
	cf "1C/Configuration"
	git "1C/Git"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

type EventBuildCfe struct {
	BeforeBuild   []func(cf.IConfiguration)
	AfterBuild    []func(ext cf.IConfiguration)
	AfterAllBuild []func() // Событие которое вызывается при сборе всех расширений
}

type BuildCfe struct {
	BaseTask
	EventBuildCfe

	ChoseExtName string
	//HideAllButtun bool
	Ext             *cf.ConfCommonData
	Branch          string
	statusMessageID int
	//end           func() // Обертка нужна что бы можно было отенить выполнение из потомка
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
		return
	}

	g := new(git.Git)
	g.RepDir = Confs.GitRep

	if err := g.ResetHard(B.Branch); err != nil {
		B.bot.Send(tgbotapi.NewEditMessageText(B.ChatID, B.statusMessageID, "Произошла ошибка при получении данных из Git: "+err.Error()))
		return
	}

	B.next(fmt.Sprintf("Данные обновлены из Git (ветка %q).\nНачинаю собирать расширения.", B.Branch))
}

func (B *BuildCfe) Invoke() {
	sendError := func(Msg string) {
		logrus.WithField("Каталог сохранения расширений", B.Ext.OutDir).Error(Msg)
		B.bot.Send(tgbotapi.NewEditMessageText(B.ChatID, B.statusMessageID, Msg))
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
		B.invokeEndTask(reflect.TypeOf(B).String())
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

	// на тек. этапе B.ChoseExtName может быть пустой строкой (в случае выбора всех расширений)
	// по этому передаем на сторону сборки, там будут имена
	beforeBuild := func(ext cf.IConfiguration) {
		// вызываем события
		for _, f := range B.BeforeBuild {
			f(ext)
		}
	}

	// обязательно в отдельной горутине, или размер канала chError делать = кол-ву расширений
	wgError.Add(1)
	go func() {
		defer wgError.Done()
		for err := range chError {
			sendError(fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err))
		}
	}()

	if err := B.Ext.BuildExtensions(chResult, chError, B.ChoseExtName, beforeBuild); err != nil {
		logrus.Panic(err) // в defer перехват
	}

	wg.Wait()
	wgError.Wait()

}

func (B *BuildCfe) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	B.BaseTask.Initialise(bot, update, finish)
	B.EndTask[reflect.TypeOf(B).String()] = []func(){finish}

	//B.end = B.invokeEndTask
	// B.EndTask = append(B.EndTask, func() {
	// 	Msg := fmt.Sprintf("Расширения собраны и ожидают вас в каталоге %v", B.Ext.OutDir)
	// 	B.bot.Send(tgbotapi.NewMessage(B.ChatID, Msg))
	// })
	B.AfterBuild = append(B.AfterBuild, func(ext cf.IConfiguration) {
		_, fileName := filepath.Split(ext.GetFile())

		msg := tgbotapi.NewEditMessageText(B.ChatID, B.statusMessageID, fmt.Sprintf("Собрано расширение %q", fileName))
		B.bot.Send(msg)
	})
	B.AfterAllBuild = append(B.AfterAllBuild, func() {
		B.bot.Send(tgbotapi.NewEditMessageText(B.ChatID, B.statusMessageID, fmt.Sprintf("Расширения собраны и ожидают вас в каталоге %v", B.Ext.OutDir)))
	})

	B.Ext = new(cf.ConfCommonData).New(Confs)
	firstStep := new(step).Construct("Выберите расширения", "BuildCfe-1", B, ButtonCancel|ButtonBack, 2).whenGoing(func() {
		msg, _ := B.bot.Send(tgbotapi.NewMessage(B.ChatID, "Статус"))
		B.statusMessageID = msg.MessageID
	})
	for _, ext := range B.Ext.GetExtensions() {
		name := ext.GetName()
		firstStep.appendButton(name, func() { B.ChoseExt(name) })
	}
	firstStep.appendButton("Все", B.ChoseAll).reverseButton()

	// if !B.HideAllButtun {
	// 	firstStep.appendButton("Все", B.ChoseAll).reverseButton()
	// } else {
	// 	firstStep.reverseButton()
	// }

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
		B.bot.Send(tgbotapi.NewEditMessageText(B.ChatID, B.statusMessageID, "Произошла ошибка при получении Git веток: "+err.Error()))
		//B.end()
		return B
	}
	gitStep.appendButton("Не обновлять", func() { B.ChoseBranch("") }).reverseButton()

	// Если ветка уже заполнена мы должны проскочить шаг выбора ветки.
	// Ветка может быть заполнена в случае DeployExtension
	if B.Branch != "" {
		gitStep = new(step).Construct("", "", B, 0, 2).whenGoing(func() { B.next("") })
	}

	B.steps = []IStep{
		firstStep,
		gitStep,
		new(step).Construct("⚙️ Начинаю собирать расширения.", "BuildCfe-3", B, 0, 2).whenGoing(func() {
			go B.Invoke()
		}),
	}

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
