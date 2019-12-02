package telegram

import (
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/sirupsen/logrus"
)

type SendMsg struct {
	BaseTask

	msg string
}

func (this *SendMsg) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	this.BaseTask.Initialise(bot, update, finish)
	this.AppendDescription(this.name)

	this.steps = []IStep{
		new(step).Construct("Введите сообщение", "Шаг1", this, ButtonCancel, 3).whenGoing(
			func() {
				this.hookInResponse = func(update *tgbotapi.Update) bool {
					this.msg = this.GetMessage().Text
					this.steps[this.currentStep+1].(*step).Msg = this.steps[this.currentStep].(*step).Msg
					this.next("")
					return false
				}
			},
		),
		new(step).Construct("Введите ChatID", "Шаг2", this, ButtonCancel, 3).whenGoing(
			func() {
				this.hookInResponse = func(update *tgbotapi.Update) bool {
					if ChatID, err := strconv.Atoi(strings.Trim(this.GetMessage().Text, " ")); err == nil {
						this.bot.Send(tgbotapi.NewMessage(int64(ChatID), this.msg))
						this.invokeEndTask("")
						return true
					} else {
						this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Введите число. Вы ввели %q", this.GetMessage().Text)))
						return false
					}
				}
			},
		),
	}

	return this
}

func (this *SendMsg) Start() {
	logrus.WithField("description", this.GetDescription()).Debug("Start")
	this.steps[this.currentStep].invoke(&this.BaseTask)
}

func (B *SendMsg) InfoWrapper(task ITask) {
	B.info = "ℹ Отправка сообщений"
	B.BaseTask.InfoWrapper(task)
}
