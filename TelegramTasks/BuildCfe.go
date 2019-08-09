package telegram

import (
	cf "1C/Configuration"
	git "1C/Git"
	"fmt"
	"path/filepath"
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

	ChoseExtName string
	Ext          *cf.ConfCommonData
	//notInvokeInnerFinish bool
}

func (B *BuildCfe) ChoseExt(ChoseData string) {
	B.ChoseExtName = ChoseData

	if !B.PullGit() {
		B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Начинаю собирать расширение "+ChoseData))
		go B.Invoke()
	}
}

func (B *BuildCfe) ChoseAll() {
	if !B.PullGit() {
		B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Начинаю собирать расширения."))
		go B.Invoke()
	}
}

func (B *BuildCfe) ChoseBranch(Branch string) {
	if Branch == "" {
		B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Начинаю собирать расширения."))
		go B.Invoke()
		return
	}

	g := new(git.Git)
	g.RepDir = Confs.GitRep

	if err := g.Pull(Branch); err != nil {
		B.baseFinishMsg("Произошла ошибка при получении данных из Git: " + err.Error())
	}

	B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, fmt.Sprintf("Данные обновлены из Git (ветка %q).\nНачинаю собирать расширения.", Branch)))
	go B.Invoke()
}

func (B *BuildCfe) PullGit() bool {
	if Confs.GitRep == "" {
		return false
	}

	g := new(git.Git)
	g.RepDir = Confs.GitRep

	if err, list := g.GetBranches(); err == nil {
		msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Выберите Git ветку для обновления")
		Buttons := make([]map[string]interface{}, 0)

		for _, Branch := range list {
			var BranchName string = Branch
			B.appendButton(&Buttons, Branch, func() { B.ChoseBranch(BranchName) })
		}
		B.appendButton(&Buttons, "Не обновлять", func() { B.ChoseBranch("") })

		B.createButtons(&msg, Buttons, 2, true)
		B.bot.Send(msg)
	} else {
		B.baseFinishMsg("Произошла ошибка при получении Git веток: " + err.Error())
	}

	return true
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

		go func() {
			for err := range chError {
				sendError(fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err))
			}
		}()
	}

	// вызываем события
	for _, f := range B.BeforeBuild {
		f()
	}

	if err := B.Ext.BuildExtensions(chResult, chError, B.ChoseExtName); err != nil {
		panic(err) // в defer перехват
	}

	wg.Wait()
}

func (B *BuildCfe) Ini(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
	B.state = StateWork
	B.bot = bot
	B.update = update
	B.outFinish = finish

	B.AfterBuild = append(B.AfterBuild, func(ext cf.IConfiguration) {
		_, fileName := filepath.Split(ext.GetFile())

		msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, fmt.Sprintf("Собрано расширение %q", fileName))
		B.bot.Send(msg)
	})
	B.AfterAllBuild = append(B.AfterAllBuild, B.innerFinish)

	B.AppendDescription(B.name)
	B.startInitialise(bot, update, finish)

}

func (B *BuildCfe) startInitialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
	B.Ext = new(cf.ConfCommonData).New(Confs)
	msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Выберите расширения")

	Buttons := make([]map[string]interface{}, 0)
	for _, ext := range B.Ext.GetExtensions() {
		name := ext.GetName()
		B.appendButton(&Buttons, name, func() { B.ChoseExt(name) })
	}
	B.appendButton(&Buttons, "Все", B.ChoseAll)
	B.createButtons(&msg, Buttons, 2, true)
	bot.Send(msg)
}

func (B *BuildCfe) innerFinish() {
	Msg := fmt.Sprintf("Расширения собраны и ожидают вас в каталоге %v", B.Ext.OutDir)
	B.baseFinishMsg(Msg)
}
