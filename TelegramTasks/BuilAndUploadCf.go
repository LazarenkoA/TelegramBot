package telegram

import (
	cf "1C/Configuration"
	"1C/fresh"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/sirupsen/logrus"
)

type BuilAndUploadCf struct {
	BuildCf

	freshConf *cf.FreshConf
	outСhan   chan *struct {
		file    string
		version string
	}
}

func (B *BuilAndUploadCf) ChoseMC(ChoseData string) {
	deferfunc := func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err)
			logrus.Error(Msg)
			B.bot.Send(tgbotapi.NewMessage(B.ChatID, Msg))
		}
		B.invokeEndTask(reflect.TypeOf(B).String())
	}

	for _, conffresh := range Confs.FreshConf {
		if ChoseData == conffresh.Name {
			B.freshConf = conffresh
			break
		}
	}

	pool := 1
	B.outСhan = make(chan *struct {
		file    string
		version string
	}, pool)

	go func() {
		chError := make(chan error, pool)
		wgLock := new(sync.WaitGroup)

		for c := range B.outСhan {
			wgLock.Add(1)

			fresh := new(fresh.Fresh)
			fresh.Conf = B.freshConf
			fresh.ConfComment = fmt.Sprintf("Автозагрузка, выгружено из хранилища %q, версия %v", B.ChoseRep.Path+B.ChoseRep.Name, B.versiontRep)
			fresh.VersionRep = B.versiontRep
			fresh.ConfCode = B.ChoseRep.ConfFreshName
			fresh.VersionCF = c.version

			fileDir, fileName := filepath.Split(c.file)
			go fresh.RegConfigurations(wgLock, chError, c.file, func() {
				os.RemoveAll(fileDir)
				deferfunc()
			})
			B.bot.Send(tgbotapi.NewMessage(B.ChatID, fmt.Sprintf("Загружаем конфигурацию %q в МС. Версия %v_%v", fileName, fresh.VersionCF, fresh.VersionRep)))
		}

		go func() {
			for err := range chError {
				msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err)
				logrus.Error(msg)
				B.bot.Send(tgbotapi.NewMessage(B.ChatID, msg))
			}
		}()

		wgLock.Wait()
		close(chError)
	}()

	B.AllowSaveLastVersion = false
	B.ReadVersion = true // для распаковки cf и чтения версии

	Cf := B.GetCfConf()
	Cf.OutDir, _ = ioutil.TempDir("", "1c_CF_") // переопределяем путь сохранения в темп, что бы не писалось по сети, т.к. все равно файл удалится
	B.next("")
	//B.BuildCf.Start()                           // вызываем родителя
}

func (B *BuilAndUploadCf) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	//B.BaseTask.Initialise(bot, update, finish)
	B.BuildCf.Initialise(bot, update, finish)

	B.EndTask = make(map[string][]func(), 0)
	B.EndTask[reflect.TypeOf(B).String()] = []func(){finish}

	B.AfterBuild = append([]func(){}, func() {
		B.outСhan <- &struct {
			file    string
			version string
		}{file: B.fileResult, version: B.cf.Version}
		close(B.outСhan)
	})

	firstStep := new(step).Construct("Выберите менеджер сервиса для загрузки конфигурации", "BuilAndUploadCf-1", B, ButtonCancel, 2)
	for _, conffresh := range Confs.FreshConf {
		Name := conffresh.Name // Обязательно через переменную, нужно для замыкания
		firstStep.appendButton(conffresh.Alias, func() { B.ChoseMC(Name) })
	}

	// Добавляем к шарам родителя свои, только добавить нужно вначало

	B.insertToFirst(firstStep)
	B.AppendDescription(B.name)

	return B
}

func (B *BuilAndUploadCf) Start() {
	logrus.WithField("description", B.GetDescription()).Debug("Start")
	B.steps[B.currentStep].invoke(&B.BaseTask)
}

func (B *BuilAndUploadCf) InfoWrapper(task ITask) {
	B.info = "ℹ ️Команда выгружает файл конфигурации (*.cf)\nи региструет выгруженный файл в менеджере сервиса."
	B.BaseTask.InfoWrapper(task)
}
