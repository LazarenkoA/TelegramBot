package telegram

import (
	cf "1C/Configuration"
	git "1C/Git"
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

	//dirOut               string
	ChoseExtName         string
	Ext                  *cf.ConfCommonData
	outСhan              chan string
	notInvokeInnerFinish bool
}

func (B *BuildCfe) ChoseExt(ChoseData string) {
	B.ChoseExtName = ChoseData

	if !B.PullGit() {
		B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Начинаю собирать расширение "+ChoseData))
		go B.Invoke()
	}
}

func (B *BuildCfe) ChoseAll() {
	if !B.PullGit() {
		B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Начинаю собирать расширения."))
		go B.Invoke()
	}
}

func (B *BuildCfe) ChoseBranch(Branch string) {
	if Branch == "" {
		B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Начинаю собирать расширения."))
		go B.Invoke()
		return
	}

	g := new(git.Git)
	g.RepDir = Confs.GitRep

	if err := g.Pull(Branch); err != nil {
		B.baseFinishMsg("Произошла ошибка при получении данных из Git: " + err.Error())
	}

	B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Данные обновлены из Git.\nНачинаю собирать расширения."))
	go B.Invoke()
}

func (B *BuildCfe) PullGit() bool {
	if Confs.GitRep == "" {
		return false
	}

	g := new(git.Git)
	g.RepDir = Confs.GitRep

	if err, list := g.GetBranches(); err == nil {
		msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Выберите Git ветку для обновления")
		Buttons := make([]map[string]interface{}, 0, 0)

		for _, Branch := range list {
			var BranchName string = Branch
			UUID, _ := uuid.NewV4()

			Buttons = append(Buttons, map[string]interface{}{
				"Alias": Branch,
				"ID":    UUID.String(),
				"Invoke": func() {
					B.ChoseBranch(BranchName)
				},
			})
		}

		UUID, _ := uuid.NewV4()
		Buttons = append(Buttons, map[string]interface{}{
			"Alias": "Не обновлять",
			"ID":    UUID.String(),
			"Invoke": func() {
				B.ChoseBranch("")
			},
		})

		B.CreateButtons(&msg, Buttons, 2, true)
		B.bot.Send(msg)
	} else {
		B.baseFinishMsg("Произошла ошибка при получении Git веток: " + err.Error())
	}

	return true
}

func (B *BuildCfe) Invoke() {
	sendError := func(Msg string) {
		logrus.WithField("Каталог сохранения расширений", B.Ext.OutDir).Error(Msg)
		B.baseFinishMsg(Msg)
	}

	defer func() {
		if err := recover(); err != nil {
			sendError(fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err))
		} else {
			B.innerFinish()
		}
		B.outFinish()
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
	B.state = StateWork

	B.bot = bot
	B.update = update
	B.outFinish = finish

	B.Ext = new(cf.ConfCommonData)
	B.Ext.BinPath = Confs.BinPath
	B.Ext.OutDir, _ = ioutil.TempDir(Confs.OutDir, "Ext_")
	//B.dirOut, _ = ioutil.TempDir(Confs.OutDir, "Ext_")

	msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Выберите расширения")
	//B.callback = make(map[string]func(), 0)
	Buttons := make([]map[string]interface{}, 0, 0)
	B.Ext.InitExtensions(Confs.Extensions.ExtensionsDir, B.Ext.OutDir)

	for _, ext := range B.Ext.GetExtensions() {
		name := ext.GetName()
		UUID, _ := uuid.NewV4()

		Buttons = append(Buttons, map[string]interface{}{
			"Alias": name,
			"ID":    UUID.String(),
			"Invoke": func() {
				B.ChoseExt(name)
			},
		})
	}
	Buttons = append(Buttons, map[string]interface{}{
		"Alias":  "Все",
		"ID":     "All",
		"Invoke": B.ChoseAll,
	})

	B.CreateButtons(&msg, Buttons, 2, true)
	bot.Send(msg)
}

func (B *BuildCfe) innerFinish() {
	if B.notInvokeInnerFinish {
		return
	}
	Msg := fmt.Sprintf("Расширения собраны и ожидают вас в каталоге %v", B.Ext.OutDir)
	B.baseFinishMsg(Msg)
}
