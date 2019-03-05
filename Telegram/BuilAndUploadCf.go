package telegram

import (
	cf "1C/Configuration"
	"1C/fresh"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/sirupsen/logrus"
)

type BuilAndUploadCf struct {
	BuildCf

	freshConf *cf.FreshConf
}

func (B *BuilAndUploadCf) ChoseMC(ChoseData string) {
	deferfunc := func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err)
			logrus.Error(Msg)
			B.baseFinishMsg(Msg)
		} else {
			B.innerFinish()
			B.outFinish()
		}
	}

	//B.state = StateWork

	for _, conffresh := range Confs.FreshConf {
		if ChoseData == conffresh.Name {
			B.freshConf = conffresh
			break
		}
	}

	pool := 1
	B.outСhan = make(chan string, pool)

	go func() {
		chError := make(chan error, pool)
		wgLock := new(sync.WaitGroup)

		for c := range B.outСhan {
			wgLock.Add(1)

			fresh := new(fresh.Fresh)
			fresh.Conf = B.freshConf
			fresh.ConfComment = fmt.Sprintf("Автозагрузка, выгружено из хранилища %q, версия %v", B.ChoseRep.Path, B.version)
			fresh.ConfCode = B.ChoseRep.ConfFreshName

			fileDir, fileName := filepath.Split(c)
			go fresh.RegConfigurations(wgLock, chError, c, func() {
				os.RemoveAll(fileDir)
				deferfunc()
			})
			B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, fmt.Sprintf("Загружаем конфигурацию %q в МС", fileName)))

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
	}()

	B.notInvokeInnerFinish = true                   // что бы не писалось сообщение о том, что расширения ожидают вас там-то
	B.StartInitialise(B.bot, B.update, B.outFinish) // вызываем родителя
}

func (B *BuilAndUploadCf) StartInitialiseDesc(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
	B.bot = bot
	B.update = update
	B.outFinish = finish

	msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Выберите менеджер сервиса для загрузки конфигурации")
	keyboard := tgbotapi.InlineKeyboardMarkup{}
	var Buttons = []tgbotapi.InlineKeyboardButton{}

	B.callback = make(map[string]func(ChoseData string), 0)
	for _, conffresh := range Confs.FreshConf {
		btn := tgbotapi.NewInlineKeyboardButtonData(conffresh.Alias, conffresh.Name)
		B.callback[conffresh.Name] = B.ChoseMC
		Buttons = append(Buttons, btn)
	}

	keyboard.InlineKeyboard = breakButtonsByColum(Buttons, 3)
	msg.ReplyMarkup = &keyboard
	bot.Send(msg)
}

func (B *BuilAndUploadCf) innerFinish() {
	B.baseFinishMsg("Готово!")
}
