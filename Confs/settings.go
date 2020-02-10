package conf

import (
	"encoding/json"
	"io/ioutil"
	"os"

	"github.com/sirupsen/logrus"
)

func ReadSettings(filepath string, data interface{}) (err error) {
	if _, err = os.Stat(filepath); os.IsNotExist(err) {
		logrus.WithField("файл", filepath).Panic("Конфигурационный файл не найден")
		return err
	}

	var file []byte
	file, err = ioutil.ReadFile(filepath)
	if err != nil {
		logrus.WithField("файл", filepath).WithField("Ошибка", err).Panic("Ошибка открытия файла")
		return err
	}

	err = json.Unmarshal(file, data)
	if err != nil {
		logrus.WithField("файл", filepath).WithField("Ошибка", err).Panic("Ошибка чтения конфигурационного файла")
		return err
	}

	return err
}
