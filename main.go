package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	session "1C/Confs"
	tel "1C/TelegramTasks"

	"github.com/garyburd/redigo/redis"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"golang.org/x/net/proxy"

	"github.com/sirupsen/logrus"
)

const (
	BotToken = "735761544:AAEXq6FKx9B_-CHY7WyshpmO0Zb8LWFikFQ"
)

type Hook struct {
}

func (h *Hook) Levels() []logrus.Level {
	return []logrus.Level{logrus.ErrorLevel, logrus.PanicLevel}
}
func (h *Hook) Fire(En *logrus.Entry) error {
	fmt.Println(En.Message)
	return nil
}

/* type settings struct {
	BinPath       string                          `json:"BinPath"`
	Extensions    *cf.ExtensionsSettings          `json:"Extensions"`
	Configuration *cf.ConfigurationCommonSettings `json:"Configuration"`
} */

var (
	//confFile string
	pass      string
	LogLevel  int
	TempFile  string
	redisAddr = "redis://user:@localhost:6379/0"
)

func main() {

	Tasks := new(tel.Tasks)
	Tasks.ReadSettings()

	redisConn, err := redis.DialURL(redisAddr)
	if err != nil {
		logrus.Panic("Ошибка установки соединения с redis")
	}
	Tasks.SessManager = session.NewSessionManager(redisConn)

	defer inilogrus().Stop()
	defer DeleleEmptyFile(logrus.StandardLogger().Out.(*os.File))

	if pass != "" {
		Tasks.SetPass(pass)
		fmt.Println("Пароль установлен")
		return
	}

	bot := NewBotAPI()
	if bot == nil {
		logrus.Panic("Не удалось подключить бота")
		return
	} else {
		logrus.Debug("К боту подключились")
	}

	/* info, _ := bot.GetWebhookInfo()
	fmt.Println(info) */

	http.HandleFunc("/Debug", func(w http.ResponseWriter, r *http.Request) {
		ioutil.ReadAll(r.Body)
		defer r.Body.Close()

		w.Write([]byte("Конект есть"))
		//fmt.Println("Конект есть")
	})

	updates := bot.ListenForWebhook("/")
	if net := tel.Confs.Network; net != nil {
		go http.ListenAndServe(":"+net.ListenPort, nil)
		//go http.ListenAndServeTLS(":"+net.ListenPort, "webhook_cert.pem", "webhook_pkey.key", nil) // для SSL
		logrus.Info("Слушаем порт " + net.ListenPort)
	} else {
		logrus.Panic("В настройках не определен параметр ListenPort")
		return
	}

	// получаем все обновления из канала updates
	for update := range updates {
		var Command string
		//update.Message.Photo[0].FileID
		//p := tgbotapi.NewPhotoShare(update.Message.Chat.ID, update.Message.Photo[0].FileID)
		//bot.GetFile(p)
		if update.Message != nil && update.Message.Command() != "start" {
			if ok, comment := Tasks.CheckSession(update.Message.From, update.Message.Text); !ok {
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Необходимо ввести пароль.\n"+comment))
				continue
			} else {
				if comment != "" {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, comment+", слушаюсь и повинуюсь."))
					continue
				}
			}
		}

		//	fmt.Println(update.Message.Text)
		if update.CallbackQuery != nil {
			existNew := false
			for _, t := range Tasks.GetTasks(update.CallbackQuery.From.ID) {
				if t.GetState() != tel.StateDone {
					callback := t.GetCallBack()
					call := callback[update.CallbackQuery.Data]
					if call != nil {
						call()
					}
					existNew = true
				}
			}
			if !existNew {
				bot.Send(tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "Не найдено активных заданий."))
			}
			continue
		}

		if update.Message == nil {
			logrus.Debug("Message = nil")
			continue
		}

		Command = update.Message.Command()
		logrus.WithFields(logrus.Fields{
			"Command":   Command,
			"Msg":       update.Message.Text,
			"FirstName": update.Message.From.FirstName,
			"LastName":  update.Message.From.LastName,
			"UserName":  update.Message.From.UserName,
		}).Debug()

		fromID := update.Message.From.ID
		// Чистим старые задания
		Tasks.Delete(fromID)

		switch Command {
		case "start":
			bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("Привет %v %v!", update.Message.From.FirstName, update.Message.From.LastName)))
		case "buildcf":
			task := Tasks.CreateTask(new(tel.BuildCf), Command, fromID, false)
			task.(*tel.BuildCf).AllowSaveLastVersion = true // блин криво
			task.Ini(bot, &update, func() { Tasks.Delete(fromID) })
		case "buildcfe":
			task := Tasks.CreateTask(new(tel.BuildCfe), Command, fromID, false)
			task.Ini(bot, &update, func() { Tasks.Delete(fromID) })
		case "buildanduploadcf":
			task := Tasks.CreateTask(new(tel.BuilAndUploadCf), Command, fromID, false)
			task.Ini(bot, &update, func() { Tasks.Delete(fromID) })
		case "buildanduploadcfe":
			task := Tasks.CreateTask(new(tel.BuilAndUploadCfe), Command, fromID, false)
			task.Ini(bot, &update, func() { Tasks.Delete(fromID) })
		case "getlistupdatestate":
			task := Tasks.CreateTask(new(tel.GetListUpdateState), Command, fromID, true)
			task.Ini(bot, &update, func() { Tasks.Delete(fromID) })
		case "setplanupdate":
			task := Tasks.CreateTask(new(tel.SetPlanUpdate), Command, fromID, false)
			task.Ini(bot, &update, func() { Tasks.Delete(fromID) })
		case "cancel":
			//Tasks.Reset(fromID, bot, &update, true)
			//bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Готово!"))
		default:
			// Проверяем общие хуки
			if Tasks.ExecuteHook(&update, update.Message.From.ID) {
				continue
			} else {
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Я такому необученный."))
			}
			//bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Простите, такого я не умею"))
		}

	}
}

func getFiles(rootDir, ext string) []string {
	var result []string
	f := func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() && filepath.Ext(info.Name()) == ext {
			result = append(result, path)
		}

		return nil
	}

	filepath.Walk(rootDir, f)
	return result
}

func NewBotAPI() *tgbotapi.BotAPI {
	// create a socks5 dialer

	httpClient := new(http.Client)
	if net := tel.Confs.Network; net != nil {
		logrus.Debug("Используем прокси " + net.PROXY_ADDR)

		dialer, err := proxy.SOCKS5("tcp", net.PROXY_ADDR, nil, proxy.Direct)
		if err != nil {
			logrus.WithField("Прокси", net.PROXY_ADDR).Errorf("Ошибка соединения с прокси: %q", err)
			return nil
		}
		// setup a http client
		httpTransport := &http.Transport{}
		httpTransport.Dial = dialer.Dial
		httpClient = &http.Client{Transport: httpTransport}
	}

	bot, err := tgbotapi.NewBotAPIWithClient(BotToken, httpClient)
	if err != nil {
		logrus.Errorf("Произошла ошибка при создании бота: %q", err)
		return nil
	}

	if net := tel.Confs.Network; net != nil {
		logrus.Debug("Устанавливаем хук на URL " + net.WebhookURL)

		//_, err = bot.SetWebhook(tgbotapi.NewWebhookWithCert(net.WebhookURL, "webhook_cert.pem"))
		_, err = bot.SetWebhook(tgbotapi.NewWebhook(net.WebhookURL))
		if err != nil {
			logrus.Errorf("Произошла ошибка при установки веб хука для бота: %q", err)
			return nil
		}
	} else {
		logrus.Panic("В настройках не определен параметр WebhookURL")
		return nil
	}

	//bot.Debug = true
	return bot
}

func inilogrus() *time.Ticker {
	//flag.StringVar(&confFile, "conffile", "", "Конфигурационный файл")
	flag.StringVar(&pass, "SetPass", "", "Установка нового пвроля")
	flag.IntVar(&LogLevel, "LogLevel", 3, "Уровень логирования от 2 до 5, где 2 - ошибка, 3 - предупреждение, 4 - информация, 5 - дебаг")

	flag.Parse()
	currentDir, _ := os.Getwd()
	var LogDir string

	createNewDir := func() string {
		dir := filepath.Join(LogDir, time.Now().Format("02.01.2006"))
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			os.Mkdir(dir, os.ModePerm)
		}
		return dir
	}

	if dir := tel.Confs.LogDir; dir != "" {
		LogDir = tel.Confs.LogDir
		LogDir = strings.Replace(LogDir, "%AppDir%", currentDir, -1)
		if _, err := os.Stat(LogDir); os.IsNotExist(err) {
			os.Mkdir(LogDir, os.ModePerm)
		}
	} else {
		LogDir = currentDir
	}

	Log1, _ := os.OpenFile(filepath.Join(createNewDir(), "Log_"+time.Now().Format("15.04.05")), os.O_CREATE, os.ModeAppend)
	logrus.SetOutput(Log1)

	timer := time.NewTicker(time.Minute * 10)
	go func() {
		for range timer.C {
			Log, _ := os.OpenFile(filepath.Join(createNewDir(), "Log_"+time.Now().Format("15.04.05")), os.O_CREATE, os.ModeAppend)
			oldFile := logrus.StandardLogger().Out.(*os.File)
			logrus.SetOutput(Log)
			DeleleEmptyFile(oldFile)
		}
	}()

	logrus.SetLevel(logrus.Level(LogLevel))
	logrus.AddHook(new(Hook))

	//line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	//fmt.Println(line)

	return timer
}

func DeleleEmptyFile(file *os.File) {
	// Если файл пустой, удаляем его. что бы не плодил кучу файлов
	info, _ := file.Stat()
	if info.Size() == 0 {
		file.Close()

		if err := os.Remove(file.Name()); err != nil {
			logrus.WithError(err).WithField("Файл", file.Name()).Error("Ошибка удаления пустого файла логов")
		}
	}
}

// ДЛЯ ПАПЫ
/* buildcfe - Собрать файлы расширений *.cfe
buildcf - Собрать файл конфигурации *.cf
buildanduploadcf - Собрать конфигурацию и отправить в менеджер сервиса
buildanduploadcfe - Собрать Файлы расширений и обновить в менеджер сервиса
setplanupdate - Запланировать обновление
getlistupdatestate - Получить список запланированных обновлений конфигураций
cancel - Отмена текущего действия  */
