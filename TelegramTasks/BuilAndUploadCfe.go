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
	OverriteChoseMC func(ChoseData string) // для того что бы можно было переопределить действия после выбора МС при вызове из потомка
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
			wgLock.Add(1)
			comment := fmt.Sprintf("Собрано из ветки %q", B.Branch)
			go fresh.RegExtension(wgLock, chError, c.GetFile(), comment, func(GUID string) {
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
				B.bot.Send(tgbotapi.NewMessage(B.ChatID, msg))
				//B.baseFinishMsg(msg) // не стоит этого делать
			}
		}()

		wgLock.Wait()
		close(chError)

		time.Sleep(time.Millisecond * 5)
		deferfunc() // именно так
	}()

	B.next("")
	//B.BuildCfe.Start() // вызываем родителя
}

func (B *BuilAndUploadCfe) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	B.BaseTask.Initialise(bot, update, finish)
	B.BuildCfe.Initialise(bot, update, finish)

	B.EndTask = append(B.EndTask, B.innerFinish)
	B.OverriteChoseMC = B.ChoseMC
	B.outСhan = make(chan cf.IConfiguration, pool)
	B.AfterBuild = append(B.AfterBuild, func(ext cf.IConfiguration) { B.outСhan <- ext })
	B.AfterAllBuild = append([]func(){}, func() {
		close(B.outСhan)
		B.baseFinishMsg("")
	}) // закрываем канал после сбора всех расширений

	firstStep := new(step).Construct("Выберите менеджер сервиса для загрузки расширений", "BuilAndUploadCfe-1", B, ButtonCancel, 2)
	for _, conffresh := range Confs.FreshConf {
		Name := conffresh.Name // Обязательно через переменную, нужно для замыкания
		firstStep.appendButton(conffresh.Alias, func() { B.OverriteChoseMC(Name) })
	}

	// Добавляем к шарам родителя свои, только добавить нужно вначало
	B.insertToFirst(firstStep)
	B.AppendDescription(B.name)

	return B
}

func (B *BuilAndUploadCfe) Start() {
	logrus.WithField("description", B.GetDescription()).Debug("Start")

	B.steps[B.currentStep].invoke(&B.BaseTask)
}

func (B *BuilAndUploadCfe) InfoWrapper(task ITask) {
	B.info = "ℹ Команда выгружает файл расширений (*.cfe)\nи региструет выгруженный файл в менеджере сервиса."
	B.BaseTask.InfoWrapper(task)
}

func (B *BuilAndUploadCfe) innerFinish() {
	B.baseFinishMsg(fmt.Sprintf("Задание:\n%v\nГотово!", B.GetDescription()))
	B.outFinish()
}
