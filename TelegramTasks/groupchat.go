package telegram

import (
	"fmt"
	logrusRotate "github.com/LazarenkoA/LogrusRotate"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"strings"
)

type Group struct {
	BaseTask

}

func (this *Group) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	this.BaseTask.Initialise(bot, update, finish)

	return this
}


func (this *Group) Start() {
	if this.update.Message == nil {
		return
	}

	logrusRotate.StandardLogger().
		WithField("From", this.update.Message.From).
		WithField("Chat", this.update.Message.Chat).
		WithField("Msg", this.update.Message.Text).
		Debug("Обработка событий группового чата")


	// В зависимости от чата и от отправителя разные действия
	// пока хардкорд
	//if this.update.Message.Chat.ID == -462156478 && this.update.Message.From.UserName == "PARMA_DC2_BOT" {
	if strings.ToUpper(this.update.Message.From.UserName) == "PARMA_DC2_BOT" {
		if strings.Index(this.update.Message.Text, "❗️") >= 0 {
			sui := new(SUI)
			sui.Initialise(this.bot, this.update, func() {})
			sui.subject = "Обработка инцидентов ЕИС УФХД"
			sui.ticketBody = this.update.Message.Text
			if ticketNumber, err := sui.createTicket(); err != nil {
				logrusRotate.StandardLogger().WithError(err).Error("Произошла ошибка при создании заявки в СУИ")
			} else {
				logrusRotate.StandardLogger().Info(fmt.Sprintf("Создана заявка в СУИ, номер %q", ticketNumber))
				msg := tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Создана заявка с номером %q", ticketNumber)) //ticketNumber
				msg.ReplyToMessageID = this.GetMessage().MessageID
				this.bot.Send(msg)
			}
		}
	}
}