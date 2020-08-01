package telegram

import (
	red "github.com/LazarenkoA/TelegramBot/Redis"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/sirupsen/logrus"
	"strconv"
)

type SendMsg struct {
	BaseTask

	msg string
	sticker *tgbotapi.Sticker
}

func (this *SendMsg) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	this.BaseTask.Initialise(bot, update, finish)
	this.AppendDescription(this.name)

	var redis *red.Redis
	Confs.DIContainer.Invoke(func(r *red.Redis) {
		redis = r
	})

	this.steps = []IStep{
		new(step).Construct("Введите сообщение", "Шаг1", this, ButtonCancel, 3).whenGoing(
			func(thisStep IStep) {
				this.hookInResponse = func(updateupdate *tgbotapi.Update) bool {
					msg := this.GetMessage()
					this.msg = msg.Text
					this.sticker = msg.Sticker

					this.steps[this.currentStep+1].(*step).Msg = this.steps[this.currentStep].(*step).Msg // т.к. мы ввели сообщение, оно испортило нам всю малину

					// Удаляем введенное сообщение
					bot.DeleteMessage(tgbotapi.DeleteMessageConfig{
						ChatID: msg.Chat.ID,
						MessageID: msg.MessageID })

					if this.sticker != nil {
						this.next("Кому отправить стрикер?\n")
					} else {
						this.next(fmt.Sprintf("Кому отправить сообщение\n%q?", this.msg))
					}

					return false
				}
			},
		),
		new(step).Construct("", "Шаг2", this, ButtonCancel, 1).whenGoing(
			func(thisStep IStep) {
				//this.hookInResponse = func(update *tgbotapi.Update) bool {
				//	if ChatID, err := strconv.Atoi(strings.Trim(this.GetMessage().Text, " ")); err == nil {
				//		this.bot.Send(tgbotapi.NewMessage(int64(ChatID), this.msg))
				//		this.invokeEndTask("")
				//		return true
				//	} else {
				//		this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Введите число. Вы ввели %q", this.GetMessage().Text)))
				//		return false
				//	}
				//}

				for _, v := range redis.Items("users") {
					userInfo := redis.StringMap(v)
					thisStep.appendButton(userInfo["FirstName"] + " " + userInfo["LastName"], func() {
						if ChatID, err :=  strconv.ParseInt(userInfo["ChatID"], 10, 64); err == nil {
							if this.sticker != nil {
								this.bot.Send(tgbotapi.NewStickerShare(ChatID, this.sticker.FileID))
							} else {
								this.bot.Send(tgbotapi.NewMessage(ChatID, this.msg))
							}

							this.next("")
							finish()
						}
					})
				}
			},
		).appendButton("Всем", func() {
			for _, v := range redis.Items("users") {
				userInfo := redis.StringMap(v)
				if ChatID, err :=  strconv.ParseInt(userInfo["ChatID"], 10, 64); err == nil {
					if this.sticker != nil {
						this.bot.Send(tgbotapi.NewStickerShare(ChatID, this.sticker.FileID))
					} else {
						this.bot.Send(tgbotapi.NewMessage(ChatID, this.msg))
					}
				}
			}
			this.next("")
			finish()
		}),
		new(step).Construct("Отправлено", "Шаг3", this, 0, 1),
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
