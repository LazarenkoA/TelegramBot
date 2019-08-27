package conf

import (
	"encoding/json"
	"io/ioutil"
	"os"

	"github.com/sirupsen/logrus"
)

func ReadSettings(filepath string, data interface{}) {
	if _, err := os.Stat(filepath); os.IsNotExist(err) {
		logrus.WithField("файл", filepath).Panic("Конфигурационный файл не найден")
		return
	}

	file, err := ioutil.ReadFile(filepath)
	if err != nil {
		logrus.WithField("файл", filepath).WithField("Ошибка", err).Panic("Ошибка открытия файла")
		return
	}

	err = json.Unmarshal(file, data)
	if err != nil {
		logrus.WithField("файл", filepath).WithField("Ошибка", err).Panic("Ошибка чтения конфигурационного файла")
		return
	}

	/////////////////////////////////////////////

	/* confFile = filepath.Join(currentDir, "conf")

	if confFile == "" {
		confFile = filepath.Join(currentDir, "conf")
		logrus.Warning("Конфигурационный файл не задан, поиск файла будет осушествлен в текущем каталоге")
	}
	if _, err := os.Stat(confFile); os.IsNotExist(err) {
		logrus.WithField("файл", confFile).Error("Конфигурационный файл не найден")
		return
	}

	logrus.WithField("файл", confFile).Debug("Конфигурационный файл")
	file, err = ioutil.ReadFile(confFile)

	//fmt.Println(string(file))

	if err != nil {
		logrus.WithField("файл", confFile).WithField("Ошибка", err).Error("Ошибка открытия файла")
		return
	}

	logrus.WithField("Содержимое файла", confFile).Debug(string(file))

	S = new(settings)
	err = json.Unmarshal(file, S)
	if err != nil {
		logrus.WithField("файл", confFile).WithField("Ошибка", err).Error("Ошибка чтения конфигурационного файла")
		return
	}

	if S.Extensions != nil && S.Extensions.DirOut == "" {
		S.Extensions.DirOut = currentDir
	} */
}
