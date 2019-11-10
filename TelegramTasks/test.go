package telegram

import (
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/sirupsen/logrus"
)

type Test struct {
	BaseTask
}

//////////////////////////////////////////////////////////////////

func (this *Test) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	this.BaseTask.Initialise(bot, update, finish)

	//this.navigationMsg = tgbotapi.NewMessage(this.ChatID, "")

	this.steps = []IStep{
		new(step).Construct("Шаг 1", "Шаг1", this, 0, 2).appendButton("Да", func() {
			msg := tgbotapi.NewMessage(this.ChatID, "Вы выбрали  \"Да\"")
			this.bot.Send(msg)
		}).appendButton("Нет", func() {
			msg := tgbotapi.NewMessage(this.ChatID, "Вы выбрали  \"Нет\"")
			this.bot.Send(msg)
		}),
		new(step).Construct("Шаг 2", "Шаг2", this, 0, 2).appendButton("Да", func() {
			msg := tgbotapi.NewMessage(this.ChatID, "Вы выбрали  \"Да\"")
			this.bot.Send(msg)
		}).appendButton("Нет", func() {
			msg := tgbotapi.NewMessage(this.ChatID, "Вы выбрали  \"Нет\"")
			this.bot.Send(msg)
		}),
		new(step).Construct("Шаг 3", "Шаг3", this, 0, 1),
	}

	this.AppendDescription(this.name)
	return this
}

func (this *Test) Start() {
	logrus.WithField("description", this.GetDescription()).Debug("Start")

	this.steps[this.currentStep].invoke(&this.BaseTask)
}

func (this *Test) innerFinish() {
	this.baseFinishMsg(fmt.Sprintf("Задание:\n%v\nГотово!", this.GetDescription()))
	this.outFinish()
}

func (B *Test) InfoWrapper(task ITask) {
	B.info = "ℹ Отладка новой навигации."
	B.BaseTask.InfoWrapper(task)
}
