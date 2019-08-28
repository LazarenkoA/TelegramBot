package telegram

import (
	cf "1C/Configuration"
	"1C/fresh"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/sirupsen/logrus"
)

const pool int = 5

type EventBuilAndUploadCfe struct {
	BeforeUploadFresh []func(ext cf.IConfiguration)
	AfterUploadFresh  []func(ext cf.IConfiguration)
	EndTask           []func()
}

type BuilAndUploadCfe struct {
	BuildCfe
	EventBuilAndUploadCfe

	freshConf       *cf.FreshConf
	outСhan         chan cf.IConfiguration
	overriteChoseMC func(ChoseData string) // для того что бы можно было переопределить действия после выбора МС при вызове из потомка
}

func (B *BuilAndUploadCfe) ChoseMC(ChoseData string) {
	deferfunc := func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err)
			logrus.WithField("Каталог сохранения расширений", B.Ext.OutDir).Error(Msg)
			B.baseFinishMsg(Msg)
		} else {
			// вызываем события
			if B.EndTask != nil {
				for _, f := range B.EndTask {
					f()
				}
			}
		}
	}

	for _, conffresh := range Confs.FreshConf {
		if ChoseData == conffresh.Name {
			B.freshConf = conffresh
			break
		}
	}

	go func() {
		wgLock := new(sync.WaitGroup)
		chError := make(chan error, 1)

		for c := range B.outСhan {
			wgLock.Add(1)
			fresh := new(fresh.Fresh)
			if fresh.Conf == nil { // Значение уже может быть инициализировано (из потомка)
				fresh.Conf = B.freshConf
			}

			_, fileName := filepath.Split(c.GetFile())

			// вызываем события
			for _, f := range B.BeforeUploadFresh {
				f(c)
			}

			B.bot.Send(tgbotapi.NewMessage(B.ChatID, fmt.Sprintf("Загружаем расширение %q в МС", fileName)))

			locC := c // для замыкания
			go fresh.RegExtension(wgLock, chError, c.GetFile(), func(GUID string) {
				// вызываем события после отправки
				for _, f := range B.AfterUploadFresh {
					locC.(*cf.Extension).GUID = GUID
					f(locC)
				}
			})
		}

		go func() {
			for err := range chError {
				msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err)
				logrus.Error(msg)
				B.baseFinishMsg(msg)
			}
		}()

		wgLock.Wait()
		close(chError)

		time.Sleep(time.Millisecond * 5)
		deferfunc() // именно так
	}()

	B.BuildCfe.Start() // вызываем родителя
}

func (B *BuilAndUploadCfe) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	B.BaseTask.Initialise(bot, update, finish)
	B.EndTask = append(B.EndTask, B.innerFinish)
	B.overriteChoseMC = B.ChoseMC

	B.AppendDescription(B.name)

	return B
}

func (B *BuilAndUploadCfe) Start() {
	B.outСhan = make(chan cf.IConfiguration, pool)
	B.AfterBuild = append(B.AfterBuild, func(ext cf.IConfiguration) { B.outСhan <- ext })
	B.AfterAllBuild = append(B.AfterAllBuild, func() { close(B.outСhan) }) // закрываем канал после сбора всех расширений
	// if B.overriteChoseMC == nil {
	// 	B.overriteChoseMC = B.ChoseMC
	// }

	msg := tgbotapi.NewMessage(B.ChatID, "Выберите менеджер сервиса")
	B.callback = make(map[string]func())
	Buttons := make([]map[string]interface{}, 0)

	for _, conffresh := range Confs.FreshConf {
		Name := conffresh.Name // Обязательно через переменную, нужно для замыкания
		B.appendButton(&Buttons, conffresh.Alias, func() { B.overriteChoseMC(Name) })
	}

	B.createButtons(&msg, Buttons, 3, true)
	B.bot.Send(msg)
}

func (B *BuilAndUploadCfe) InfoWrapper(task ITask) {
	B.info = "ℹ Команда выгружает файл расширений (*.cfe)\nи региструет выгруженный файл в менеджере сервиса."
	B.BaseTask.InfoWrapper(task)
}

func (B *BuilAndUploadCfe) innerFinish() {
	B.baseFinishMsg(fmt.Sprintf("<b>Задание:</b>\n%v\nГотово!", B.GetDescription()))
	B.outFinish()
}
