package telegram

import (
	cf "1C/Configuration"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"sync"

	uuid "github.com/nu7hatch/gouuid"
	"github.com/sirupsen/logrus"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

type BuildCfe struct {
	BaseTask

	dirOut               string
	ChoseExtName         string
	Ext                  *cf.ConfCommonData
	outСhan              chan string
	notInvokeInnerFinish bool
}

func (B *BuildCfe) ChoseExt(ChoseData string) {
	B.state = StateWork
	B.ChoseExtName = ChoseData

	B.ChoseExtName = ChoseData
	B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Начинаю собирать расширение "+ChoseData))
	go B.Invoke()
}

func (B *BuildCfe) ChoseAll() {
	B.state = StateWork

	B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Начинаю собирать расширения"))
	go B.Invoke()
}

func (B *BuildCfe) Invoke() {
	sendError := func(Msg string) {
		logrus.WithField("Каталог сохранения расширений", B.dirOut).Error(Msg)
		B.baseFinishMsg(Msg)
	}

	defer func() {
		if err := recover(); err != nil {
			sendError(fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err))
		} else {
			B.innerFinish()
			B.outFinish()
		}
	}()

	wg := new(sync.WaitGroup)
	pool := 5
	chExt := make(chan string, pool)
	chError := make(chan error, pool)

	for i := 0; i < pool; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for c := range chExt {
				if B.outСhan != nil {
					B.outСhan <- c
				}
				_, fileName := filepath.Split(c)
				msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, fmt.Sprintf("Собрано расщирение %q", fileName))
				go B.bot.Send(msg)
			}
		}()

		go func() {
			for err := range chError {
				B.notInvokeInnerFinish = true
				sendError(fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err))
			}
		}()
	}

	err := B.Ext.BuildExtensions(chExt, chError, B.ChoseExtName)

	if err != nil {
		panic(err) // в defer перехват
	}

	wg.Wait()
	if B.outСhan != nil {
		close(B.outСhan)
	}

}

func (B *BuildCfe) StartInitialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
	B.bot = bot
	B.update = update
	B.outFinish = finish

	B.Ext = new(cf.ConfCommonData)
	B.Ext.BinPath = Confs.BinPath
	B.dirOut, _ = ioutil.TempDir("", "Ext_")

	msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Выберите расширения")
	keyboard := tgbotapi.InlineKeyboardMarkup{}
	var Buttons = []tgbotapi.InlineKeyboardButton{}

	B.callback = make(map[string]func(), 0)
	B.Ext.InitExtensions(Confs.Extensions.ExtensionsDir, B.dirOut)

	for _, ext := range B.Ext.GetExtensions() {
		name := ext.GetName()
		UUID, _ := uuid.NewV4()
		btn := tgbotapi.NewInlineKeyboardButtonData(name, UUID.String())
		B.callback[UUID.String()] = func() {
			B.ChoseExt(name)
		}
		Buttons = append(Buttons, btn)
	}

	btn := tgbotapi.NewInlineKeyboardButtonData("Все", "All")
	B.callback["All"] = B.ChoseAll
	Buttons = append(Buttons, btn)

	keyboard.InlineKeyboard = B.breakButtonsByColum(Buttons, 3)
	msg.ReplyMarkup = &keyboard
	bot.Send(msg)
}

func (B *BuildCfe) innerFinish() {
	if B.notInvokeInnerFinish {
		return
	}
	Msg := fmt.Sprintf("Расширения собраны и ожидают вас в каталоге %v", B.dirOut)
	B.baseFinishMsg(Msg)
}
