package fresh

import (
	cf "1C/Configuration"
	n "1C/Net"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type Fresh struct {
	Conf          *cf.FreshConf
	ConfCode      string
	ConfComment   string
	VersionCF     string
	VersionRep    int
	tempFile      string
	ConfFreshName string
	fileSize      int64
}

func (f *Fresh) Construct(conf *cf.FreshConf) *Fresh {
	f.Conf = conf

	return f
}

func (f *Fresh) upLoadFile(fileName string) error {
	if _, err := os.Stat(fileName); os.IsNotExist(err) {
		return fmt.Errorf("Не найден файл %v", fileName)
	}
	file, err := os.Open(fileName)
	if err != nil {
		return err
	}
	defer file.Close()

	logrus.WithFields(map[string]interface{}{
		"Сервис": f.Conf.SM.URL + f.Conf.SM.GetService("UpLoadFileServiceURL"),
		"Login":  f.Conf.SM.Login,
		"Pass":   f.Conf.SM.Pass,
	}).Info("Загружаем файл во фреш")

	MByteCount := 5
	info, _ := file.Stat() //os.Stat(fileName)
	LenBuf := (1024 * 1024) * MByteCount
	f.fileSize = info.Size()
	//SizeMb := info.Size() / 1024 / 1024

	logrus.WithFields(logrus.Fields{
		"Размер частей в Mb":           MByteCount,
		"Размер файла в byte":          f.fileSize,
		"Количество итераций загрузки": (f.fileSize / int64(LenBuf)) + 1,
	}).Debug("Информация о файле")

	buf := make([]byte, LenBuf)
	pos, err := file.Read(buf)
	for err != io.EOF {
		if e := f.sendByte(buf[:pos]); e != nil {
			return e
		}
		pos, err = file.Read(buf)
	}

	logrus.WithField("файл", fileName).Info("Файл загружен")
	return nil
}

func (f *Fresh) RegConfigurations(wg *sync.WaitGroup, chError chan error, filename string, callBack func()) {
	defer wg.Done()
	if callBack != nil {
		defer callBack()
	}
	defer func() {
		if err := recover(); err != nil {
			chError <- fmt.Errorf("Произошла ошибка при регистрации конфигурации в МС: %q", err)
		}
	}()

	logrus.WithField("файл", filename).Info("Отправляем конфигурацию во фреш")
	if err := f.upLoadFile(filename); err == nil {
		url := fmt.Sprintf("%v%v?FileName=%v&ConfCode=%v", f.Conf.SM.URL, f.Conf.SM.GetService("RegConfigurationServiceURL"), f.tempFile, f.ConfCode)
		f.callService("GET", url, f.Conf.SM, time.Minute*5)
	} else {
		panic(err) // в defer есть перехват
	}

	logrus.WithField("файл", filename).Info("Файл загружен")
}

func (f *Fresh) RegExtension(wg *sync.WaitGroup, chError chan<- error, filename, comment string, InvokeBefore func(GUID string)) {
	defer wg.Done()

	defer func() {
		logrus.WithField("Файл", filename).Debug("Удаляем файл")
		os.Remove(filename)
	}()
	defer func() {
		if err := recover(); err != nil {
			chError <- fmt.Errorf("Произошла ошибка при регистрации расширения в МС: %q", err)
		}
	}()

	logrus.WithField("файл", filename).Info("Регистрируем расширение во фреше")

	if err := f.upLoadFile(filename); err == nil {
		Url, err := url.Parse(f.Conf.SM.URL)
		if err != nil {
			logrus.Panic("Ошибка разбора URL менеджера сервиса")
		}

		Url.Path += f.Conf.SM.GetService("RegExtensionServiceURL")
		parameters := url.Values{}
		parameters.Add("FileName", f.tempFile)
		parameters.Add("comment", comment)
		Url.RawQuery = parameters.Encode()

		if extRef, err := f.callService("GET", Url.String(), f.Conf.SM, time.Minute); err == nil {
			InvokeBefore(extRef)
		} else {
			chError <- err
			return
		}
	} else {
		panic(fmt.Errorf("Не удалось загрузить файл в МС, ошибка: %v", err)) // в defer перехват и в канал
	}

	logrus.WithField("файл", filename).Info("Расширение установлено")
}

func (f *Fresh) callService(method string, ServiceURL string, Auth cf.IFreshAuth, Timeout time.Duration) (result string, err error) {
	netU := new(n.NetUtility).Construct(ServiceURL, Auth.GetLogin(), Auth.GetPass())
	if f.ConfComment != "" {
		netU.Header["Msg"] = f.ConfComment
	}
	if f.fileSize > 0 {
		netU.Header["Size"] = fmt.Sprintf("%d", f.fileSize)
	}
	if f.VersionRep > 0 {
		netU.Header["VersionRep"] = fmt.Sprintf("%d", f.VersionRep)
	}
	if f.VersionCF != "" {
		netU.Header["VersionCF"] = fmt.Sprintf("%v", f.VersionCF)
	}

	return netU.CallHTTP(method, Timeout)
}

func (f *Fresh) sendByte(b []byte) error {
	url := f.Conf.SM.URL + f.Conf.SM.GetService("UpLoadFileServiceURL")

	netU := new(n.NetUtility).Construct(url, f.Conf.SM.Login, f.Conf.SM.Pass)
	netU.Header["TempFile"] = f.tempFile

	callback := func(resp *http.Response) {
		f.tempFile = resp.Header.Get("TempFile")
	}
	return netU.SendByte("PUT", b, callback)
}

func (f *Fresh) GetListUpdateState(shiftDate int) (result string, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("Ошибка получения списка обновления: %v", e)
		}
	}()

	ServiceURL := f.Conf.SA.URL + f.Conf.SA.GetService("GetListUpdateState") + fmt.Sprintf("?shift=%d", shiftDate)
	return f.callService("GET", ServiceURL, f.Conf.SA, time.Minute*2)
}

func (f *Fresh) GeUpdateState(UUID string) (result string, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("Ошибка получения информации по обновлению: %v", e)
		}
	}()

	ServiceURL := f.Conf.SA.URL + f.Conf.SA.GetService("GeUpdateState") + "?Ref=" + UUID
	return f.callService("GET", ServiceURL, f.Conf.SA, time.Minute*2)
}

func (f *Fresh) GetAvailableUpdates(UUIDBase string, AllNew bool) (result string) {
	ServiceURL := f.Conf.SM.URL + f.Conf.SM.GetService("GetAvailableUpdates") + fmt.Sprintf("?Base=%v&AllNew=%v", UUIDBase, AllNew)
	result, _ = f.callService("GET", ServiceURL, f.Conf.SM, time.Second*30)
	return
}

// Метод возвращает все базы
func (f *Fresh) GetDatabase() (result string) {
	ServiceURL := f.Conf.SM.URL + f.Conf.SM.GetService("GetDatabase")
	result, _ = f.callService("GET", ServiceURL, f.Conf.SM, time.Second*30)
	return
}

func (f *Fresh) GetAllExtension() (result string) {
	ServiceURL := f.Conf.SM.URL + f.Conf.SM.GetService("GetAllExtension")
	result, _ = f.callService("GET", ServiceURL, f.Conf.SM, time.Second*30)
	return
}

func (f *Fresh) GetExtensionByDatabase(Base_ID string) (result string) {
	ServiceURL := f.Conf.SM.URL + f.Conf.SM.GetService("GetExtensionByDatabase") + fmt.Sprintf("?Base=%v", Base_ID)
	result, _ = f.callService("GET", ServiceURL, f.Conf.SM, time.Second*30)
	return
}

// Метод возвращает базы для которых подходит расширение переданое параметром
func (f *Fresh) GetDatabaseByExtension(extName string) (result string) {
	ServiceURL := f.Conf.SM.URL + f.Conf.SM.GetService("GetDatabaseByExtension") + "?ExtName=" + extName
	result, _ = f.callService("GET", ServiceURL, f.Conf.SM, time.Second*30)
	return
}

func (f *Fresh) SetUpdetes(UUID string, UUIDBase string, MinuteShift int, force bool, funcDefer func()) (err error) {
	if funcDefer != nil {
		defer funcDefer()
	}
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("Произошла ошибка при вызове SetUpdetes: %v", e)
		}
	}()

	//start := time.Now().Add(time.Minute * time.Duration(MinuteShift))
	//start.Format("20060102230000")

	ServiceURL := f.Conf.SM.URL + f.Conf.SM.GetService("SetUpdetes") + fmt.Sprintf("?UpdateUUID=%v&MinuteShift=%v&Base=%v&Force=%v", UUID, MinuteShift, UUIDBase, force)
	f.callService("PUT", ServiceURL, f.Conf.SM, time.Minute)

	return nil
}
