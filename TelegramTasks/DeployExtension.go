package telegram

import (
	"fmt"

	//cf "1C/Configuration"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	//"github.com/sirupsen/logrus"
)

type DeployExtension struct {
	BuilAndUploadCfe
}

// func (this *DeployExtension) Create(ancestor *BuilAndUploadCfe) *DeployExtension {
// 	this.ancestor = ancestor

// 	return this
// }

func (this *DeployExtension) Ini(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
	this.bot = bot
	this.update = update
	this.outFinish = finish
	this.state = StateWork
	this.AppendDescription(this.name)
	this.startInitialise_3(bot, update, finish)
}

func (this *DeployExtension) startInitialise_3(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
	// this.BeforeBuild = func(ext cf.IConfiguration) {
	// 	if err := ext.IncVersion(); err != nil {
	// 		logrus.WithField("Расширение", ext.GetName()).Error(err)
	// 	} else {
	// 		this.CommitAndPush(ext.(*cf.Extension).ConfigurationFile)
	// 	}
	// }
	this.startInitialise_2(bot, update, finish) // метод предка
}

func (this *DeployExtension) CommitAndPush(filePath string) bool {
	if Confs.GitRep == "" {
		return false
	}

	// g := new(git.Git)
	// g.RepDir = Confs.GitRep

	// if err, list := g.GetBranches(); err == nil {
	// 	msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Выберите Git ветку для обновления")
	// 	Buttons := make([]map[string]interface{}, 0, 0)

	// 	for _, Branch := range list {
	// 		var BranchName string = Branch
	// 		B.appendButton(&Buttons, Branch, func() { B.ChoseBranch(BranchName) })
	// 	}
	// 	B.appendButton(&Buttons, "Не обновлять", func() { B.ChoseBranch("") })

	// 	B.createButtons(&msg, Buttons, 2, true)
	// 	B.bot.Send(msg)
	// } else {
	// 	B.baseFinishMsg("Произошла ошибка при получении Git веток: " + err.Error())
	// }

	return true
}

func (this *DeployExtension) innerFinish() {
	this.baseFinishMsg(fmt.Sprintf("Задание:\n%v\nГотово!", this.description))
	this.outFinish()
}
