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

type BuilAndUploadCfe struct {
	BuildCfe

	freshConf *cf.FreshConf
}

func (B *BuilAndUploadCfe) ChoseMC(ChoseData string) {
	deferfunc := func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err)
			logrus.WithField("Каталог сохранения расширений", B.dirOut).Error(Msg)
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

	pool := 5
	B.outСhan = make(chan string, pool)
	chError := make(chan error, pool)

	for i := 0; i < pool; i++ {
		go func() {
			wgLock := new(sync.WaitGroup)
			for c := range B.outСhan {
				wgLock.Add(1)
				fresh := new(fresh.Fresh)
				fresh.Conf = B.freshConf
				fileDir, fileName := filepath.Split(c)

				B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, fmt.Sprintf("Загружаем расширение %q в МС", fileName)))
				go fresh.RegExtension(wgLock, chError, c, func() {
					os.RemoveAll(fileDir)
					deferfunc()
				})
			}
			wgLock.Wait()
		}()

		go func() {
			for err := range chError {
				msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err)
				logrus.Error(msg)
				B.baseFinishMsg(msg)
			}
		}()
	}

	B.notInvokeInnerFinish = true                   // что бы не писалось сообщение о том, что расширения ожидают вас там-то
	B.StartInitialise(B.bot, B.update, B.outFinish) // вызываем родителя
}

func (B *BuilAndUploadCfe) StartInitialiseDesc(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
	B.bot = bot
	B.update = update
	B.outFinish = finish

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Выберите менеджер сервиса для загрузки расширений")
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

func (B *BuilAndUploadCfe) innerFinish() {
	B.baseFinishMsg("Готово!")
}
