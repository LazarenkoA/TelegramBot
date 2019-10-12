package fresh

import (
	cf "1C/Configuration"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
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
		"Количество итераций загрузки": f.fileSize / int64(LenBuf),
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

		extRef := f.callService("GET", Url.String(), f.Conf.SM, time.Minute)
		InvokeBefore(extRef)
	} else {
		panic(fmt.Errorf("Не удалось загрузить файл в МС, ошибка: %v", err)) // в defer перехват и в канал
	}

	logrus.WithField("файл", filename).Info("Расширение установлено")
}

func (f *Fresh) callService(method string, ServiceURL string, Auth cf.IFreshAuth, Timeout time.Duration) (result string) {
	logrus.Infof("Вызываем URL %v", ServiceURL)

	req, err := http.NewRequest(method, ServiceURL, nil)
	if err != nil {
		logrus.WithField("Сервис", ServiceURL).Errorf("Произошла ошибка при регистрации запроса: %v", err)
		panic(fmt.Errorf("Произошла ошибка при загрузки файла: %v", err))
	}
	req.SetBasicAuth(Auth.GetLogin(), Auth.GetPass())
	if f.ConfComment != "" {
		req.Header.Add("Msg", f.ConfComment)
	}
	if f.fileSize > 0 {
		req.Header.Add("Size", fmt.Sprintf("%d", f.fileSize))
	}
	if f.VersionRep > 0 {
		req.Header.Add("VersionRep", fmt.Sprintf("%d", f.VersionRep))
	}
	if f.VersionCF != "" {
		req.Header.Add("VersionCF", fmt.Sprintf("%v", f.VersionCF))
	}

	client := &http.Client{Timeout: Timeout}
	resp, err := client.Do(req)
	if err != nil {
		logrus.WithField("Сервис", ServiceURL).Errorf("Произошла ошибка при выполнении запроса: %v", err)
		panic(fmt.Errorf("Произошла ошибка при загрузки файла: %v", err))
	}
	if resp != nil {
		if err, result = f.readResp(resp); err != nil {
			panic(err) // выше по колстеку есть перехват
		}
	}
	return result
}

func (f *Fresh) sendByte(b []byte) error {
	logrus.Debugf("Отправляем %v байт", len(b))

	/* requestBody := new(bytes.Buffer)
	multiPartWriter := multipart.NewWriter(requestBody)
	mPW, _ := multiPartWriter.CreateFormField("byte")
	mPW.Write(b)
	multiPartWriter.Close() */

	//req, err := http.NewRequest("PUT", f.Conf.UpLoadFileServiceURL, requestBody)
	url := f.Conf.SM.URL + f.Conf.SM.GetService("UpLoadFileServiceURL")
	req, err := http.NewRequest("PUT", url, bytes.NewReader(b))
	if err != nil {
		logrus.WithField("Сервис", url).Errorf("Произошла ошибка при регистрации запроса: %v", err)
		return err
	}
	req.SetBasicAuth(f.Conf.SM.Login, f.Conf.SM.Pass)

	//req.Header.Set("Content-Type", multiPartWriter.FormDataContentType())
	req.Header.Add("TempFile", f.tempFile)

	client := &http.Client{Timeout: time.Minute * 10}
	resp, err := client.Do(req)
	if err != nil {
		logrus.WithField("Сервис", url).Errorf("Произошла ошибка при выполнении запроса: %v", err)
		return err
	}

	f.tempFile = resp.Header.Get("TempFile")
	err, _ = f.readResp(resp)
	return err
}

func (f *Fresh) readResp(resp *http.Response) (error, string) {
	defer resp.Body.Close()

	location, _ := resp.Location()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logrus.WithField("location", location).Errorf("Произошла ошибка при чтении Body: %v", err)
		return err, ""
	}
	if !(resp.StatusCode >= http.StatusOK && resp.StatusCode <= http.StatusIMUsed) {
		logrus.WithField("body", string(body)).WithField("location", location).Errorf("Код ответа %v", resp.StatusCode)
		return fmt.Errorf("Код возврата %v", resp.StatusCode), ""
	}
	return nil, string(body)
}

func (f *Fresh) GetListUpdateState(DateString string) (err error, result string) {
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("Ошибка получения списка обновления: %v", e)
		}
	}()

	ServiceURL := f.Conf.SA.URL + f.Conf.SA.GetService("GetListUpdateState") + "?Date=" + DateString
	return nil, f.callService("GET", ServiceURL, f.Conf.SA, time.Second*10)
}

func (f *Fresh) GeUpdateState(UUID string) (err error, result string) {
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("Ошибка получения информации по обновлению: %v", e)
		}
	}()

	ServiceURL := f.Conf.SA.URL + f.Conf.SA.GetService("GeUpdateState") + "?Ref=" + UUID
	return nil, f.callService("GET", ServiceURL, f.Conf.SA, time.Second*10)
}

func (f *Fresh) GetAvailableUpdates(UUIDBase string, AllNew bool) string {
	ServiceURL := f.Conf.SM.URL + f.Conf.SM.GetService("GetAvailableUpdates") + fmt.Sprintf("?Base=%v&AllNew=%v", UUIDBase, AllNew)
	return f.callService("GET", ServiceURL, f.Conf.SM, time.Second*10)
}

func (f *Fresh) GetDatabase() string {
	ServiceURL := f.Conf.SM.URL + f.Conf.SM.GetService("GetDatabase")
	return f.callService("GET", ServiceURL, f.Conf.SM, time.Second*10)
}

func (f *Fresh) GetAllExtension() string {
	ServiceURL := f.Conf.SM.URL + f.Conf.SM.GetService("GetAllExtension")
	return f.callService("GET", ServiceURL, f.Conf.SM, time.Second*10)
}

func (f *Fresh) GetAvailableDatabase(extName string) string {
	ServiceURL := f.Conf.SM.URL + f.Conf.SM.GetService("GetAvailableDatabase") + "?ExtName=" + extName
	return f.callService("GET", ServiceURL, f.Conf.SM, time.Second*10)
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
