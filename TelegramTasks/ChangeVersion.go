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

type ChangeVersion struct {
	BaseTask

	ChoseExtName         string
	Ext                  *cf.ConfCommonData
	outСhan              chan string
	notInvokeInnerFinish bool
}

func (this *ChangeVersion) ChoseExt(ChoseData string) {
	this.ChoseExtName = ChoseData

	if !this.PullGit() {
		this.bot.Send(tgbotapi.NewMessage(this.GetMessage().Chat.ID, "Начинаю собирать расширение "+ChoseData))
		go this.Invoke()
	}
}

func (this *ChangeVersion) ChoseAll() {
	if !this.PullGit() {
		this.bot.Send(tgbotapi.NewMessage(this.GetMessage().Chat.ID, "Начинаю собирать расширения."))
		go this.Invoke()
	}
}

func (this *ChangeVersion) ChoseBranch(Branch string) {
	if Branch == "" {
		this.bot.Send(tgbotapi.NewMessage(this.GetMessage().Chat.ID, "Начинаю собирать расширения."))
		go this.Invoke()
		return
	}

	g := new(git.Git)
	g.RepDir = Confs.GitRep

	if err := g.Pull(Branch); err != nil {
		this.baseFinishMsg("Произошла ошибка при получении данных из Git: " + err.Error())
	}

	this.bot.Send(tgbotapi.NewMessage(this.GetMessage().Chat.ID, "Данные обновлены из Git.\nНачинаю собирать расширения."))
	go this.Invoke()
}

func (this *ChangeVersion) PullGit() bool {
	if Confs.GitRep == "" {
		return false
	}

	g := new(git.Git)
	g.RepDir = Confs.GitRep

	if err, list := g.GetBranches(); err == nil {
		msg := tgbotapi.NewMessage(this.GetMessage().Chat.ID, "Выберите Git ветку для обновления")
		Buttons := make([]map[string]interface{}, 0, 0)

		for _, Branch := range list {
			var BranchName string = Branch
			this.appendButton(&Buttons, Branch, func() { this.ChoseBranch(BranchName) })
		}
		this.appendButton(&Buttons, "Не обновлять", func() { this.ChoseBranch("") })

		this.createButtons(&msg, Buttons, 2, true)
		this.bot.Send(msg)
	} else {
		this.baseFinishMsg("Произошла ошибка при получении Git веток: " + err.Error())
	}

	return true
}

func (this *ChangeVersion) Invoke() {
	sendError := func(Msg string) {
		logrus.WithField("Каталог сохранения расширений", this.Ext.OutDir).Error(Msg)
		this.baseFinishMsg(Msg)
	}

	defer func() {
		if err := recover(); err != nil {
			sendError(fmt.Sprintf("Произошла ошибка при выполнении %q: %v", this.name, err))
		} else {
			this.innerFinish()
		}
		this.outFinish()
	}()

	wg := new(sync.WaitGroup)
	pool := 5
	chExt := make(chan string, pool)
	chError := make(chan error, pool)

	for i := 0; i < pool; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for c := range chExt {
				if this.outСhan != nil {
					this.outСhan <- c
				}
				_, fileName := filepath.Split(c)
				msg := tgbotapi.NewMessage(this.GetMessage().Chat.ID, fmt.Sprintf("Собрано расширение %q", fileName))
				go this.bot.Send(msg)
			}
		}()

		go func() {
			for err := range chError {
				this.notInvokeInnerFinish = true
				sendError(fmt.Sprintf("Произошла ошибка при выполнении %q: %v", this.name, err))
			}
		}()
	}

	err := this.Ext.BuildExtensions(chExt, chError, this.ChoseExtName)

	if err != nil {
		panic(err) // в defer перехват
	}

	wg.Wait()
	if this.outСhan != nil {
		close(this.outСhan)
	}

}

func (this *ChangeVersion) Ini(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
	this.state = StateWork
	this.bot = bot
	this.update = update
	this.outFinish = finish
	this.AppendDescription(this.name)
	this.startInitialise(bot, update, finish)

}

func (this *ChangeVersion) startInitialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
	this.Ext = new(cf.ConfCommonData)
	this.Ext.BinPath = Confs.BinPath

	msg := tgbotapi.NewMessage(this.GetMessage().Chat.ID, "Перенос версии расширениий из ветки Dev в master и инкремент версии в Dev\n Выберите расширения")
	Buttons := make([]map[string]interface{}, 0, 0)
	this.Ext.InitExtensions(Confs.Extensions.ExtensionsDir, this.Ext.OutDir)

	for _, ext := range this.Ext.GetExtensions() {
		name := ext.GetName()
		this.appendButton(&Buttons, name, func() { this.ChoseExt(name) })
	}
	this.appendButton(&Buttons, "Все", this.ChoseAll)
	this.createButtons(&msg, Buttons, 2, true)
	bot.Send(msg)
}

func (this *ChangeVersion) innerFinish() {
	if this.notInvokeInnerFinish {
		return
	}
	Msg := fmt.Sprintf("Расширения собраны и ожидают вас в каталоге %v", this.Ext.OutDir)
	this.baseFinishMsg(Msg)
}
