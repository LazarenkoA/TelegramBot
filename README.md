# TelegramBot
Телеграм бот взаимодействует с 1C:Fresh, отправляет в него cf, cfe, планирует обновление нод

Боту можно устанавливать пароль, установка осуществляется через параметр -SetPass. Сессия длится час, для сессии используется Redis.
Актуальная версия Redis для windows лежит [тут](https://github.com/MicrosoftArchive/redis/releases)

## Команды бота

**buildcfe** 					- Собрать файлы расширений *.cfe

**buildcf** 					- Собрать файл конфигурации *.cf

**buildanduploadcf** 	- Собрать конфигурацию и отправить в менеджер сервиса

**buildanduploadcfe** - Собрать Файлы расширений и обновить в менеджер сервиса

**setplanupdate** 		- Запланировать обновление

**getlistupdatestate** - Получить список запланированных обновлений конфигураций
