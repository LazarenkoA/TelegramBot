package telegram

import (
	"errors"
	"fmt"
	"os"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/sirupsen/logrus"
)

type Charts struct {
	BaseTask
}

type IChart interface {
	Build() (string, error)
}

var errorNotData error = errors.New("Пустые данные")

func (this *Charts) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	this.BaseTask.Initialise(bot, update, finish)
	this.AppendDescription(this.name)

	this.steps = []IStep{
		new(step).Construct("Выберите график", "Шаг1", this, ButtonCancel, 3).
			appendButton("Не обновленные ОД", func() {
				this.goTo(2, "")
				go this.buildChart(new(chartNotUpdatedNode))
			}).
			appendButton("Очередь сообщений", func() {
				this.goTo(2, "")
				go this.buildChart(new(chartQueueMessage))
			}),
		// appendButton("Прочее...", func() {
		// 	this.next("")
		// }),
		new(step).Construct("Пока не реализовано", "Шаг2", this, ButtonBack|ButtonCancel, 3),
		new(step).Construct("Запрашиваем данные", "Шаг3", this, 0, 3),
	}

	return this
}

func (this *Charts) buildChart(object IChart) {
	defer func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", this.name, err)
			logrus.Error(Msg)
			this.bot.Send(tgbotapi.NewMessage(this.ChatID, Msg))
		}
		this.invokeEndTask("")
	}()

	if file, err := object.Build(); err != nil && err != errorNotData {
		panic(err)
	} else if err == errorNotData {
		this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Сервис вернул пустые данные, вероятно нет не обновленных областей"))
	} else {
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			msg := tgbotapi.NewPhotoUpload(this.ChatID, file)
			this.bot.Send(msg)

			os.Remove(file)
		}
	}

}

func (this *Charts) Start() {
	logrus.WithField("description", this.GetDescription()).Debug("Start")
	this.steps[this.currentStep].invoke(&this.BaseTask)
}

func (B *Charts) InfoWrapper(task ITask) {
	B.info = "ℹ Построение различных графиков."
	B.BaseTask.InfoWrapper(task)
}
