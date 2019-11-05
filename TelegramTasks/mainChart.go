package telegram

import (
	"fmt"
	"os"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/sirupsen/logrus"
)

type Charts struct {
	BaseTask
}

func (this *Charts) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	this.BaseTask.Initialise(bot, update, finish)

	this.AppendDescription(this.name)
	return this
}

func (this *Charts) buildChartNotUpdate() {
	defer func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", this.name, err)
			logrus.Error(Msg)
			this.baseFinishMsg(Msg)
		} else {
			this.innerFinish()
		}
	}()

	object := new(chartNotUpdatedNode)
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

	msg := tgbotapi.NewMessage(this.ChatID, "Выберите график")
	Buttons := make([]map[string]interface{}, 0)
	this.appendButton(&Buttons, "Не обновленные ОД", func() {
		this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Запрашиваем данные"))
		go this.buildChartNotUpdate()
	})
	this.appendButton(&Buttons, "Прочее...", func() {
		this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Пока не реализовано"))
	})

	this.createButtons(&msg, Buttons, 3, true)
	this.bot.Send(msg)
}

func (this *Charts) innerFinish() {
	this.baseFinishMsg(fmt.Sprintf("Задание:\n%v\nГотово!", this.GetDescription()))
	this.outFinish()
}

func (B *Charts) InfoWrapper(task ITask) {
	B.info = "ℹ Построение различных графиков."
	B.BaseTask.InfoWrapper(task)
}
