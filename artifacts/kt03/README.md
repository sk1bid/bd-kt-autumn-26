# Результаты КТ-03

Объяснение запросов и экспериментов — в [отчёте](../../docs/kt03/01-report.md). Здесь сохранены данные проверок и измерений.

| Результат | Файл |
|---|---|
| Сравнение времени до и после оптимизации | [Таблица CSV](load/bench/comparison.csv), [все измерения](load/bench/measurements.json) |
| Отчёт по маршрутам | [План до](load/plans/schedule-before-01.json), [план после](load/plans/schedule-after-01.json) |
| История редкого устройства | [План до](load/plans/history_rare-before-01.json), [план после](load/plans/history_rare-after-01.json) |
| Пять бизнес-запросов | [Параметры и результаты](load/queries/) |
| Набор разработки 75 000 строк | [Проверка данных](dev/verification-dev.json), [контрольные суммы](manifest-dev.json) |
| Нагрузочный набор 3 млн строк | [Проверка данных](load/verification-load.json), [контрольные суммы](manifest-load.json) |
| Успешный COMMIT | [Состояния до и после](scenarios/commit.json) |
| Контролируемый ROLLBACK | [Сравнение состояний](scenarios/rollback.json), [ошибка и откат](scenarios/rollback.txt) |
| Конкурентное выполнение | [Итог двух сессий](scenarios/concurrency.json), [диагностика ожидания](scenarios/lock-wait.json) |
| INSERT и COPY | [Измерения загрузки](load-comparison.json) |
| Целостность и бизнес-правила | [Регрессия](regression.json), [проверки БП-10](business-regression.json) |
| Проверка стоимости загрузки | [Малые пробные наборы](pilot.json) |
| Параметры стенда | [Версии и настройки](environment.json) |

В `load/plans/` сохранён каждый из семи измеряемых запусков. Окончание `-01.json` обозначает первый запуск; для начала достаточно планов по ссылкам выше.
