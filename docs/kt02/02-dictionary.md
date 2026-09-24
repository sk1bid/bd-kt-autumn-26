# Словарь данных

Таблиц: 24. NN — NOT NULL. Типы и ключи взяты из каталога PostgreSQL после применения всех миграций.

## app_user

Учётная запись сотрудника перевозчика.

FOREIGN KEY (driver_id) REFERENCES transport.driver(id).
UNIQUE (driver_id).
UNIQUE (login).
PRIMARY KEY (id).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| id | bigint NN | Идентификатор записи; GENERATED ALWAYS AS IDENTITY |
| login | text NN | Уникальный логин в нижнем регистре |
| full_name | text NN | ФИО сотрудника |
| driver_id | bigint NULL | Ссылка на водителя |
| active | boolean NN | Учётная запись активна; DEFAULT true |

## assignment

История включения планового рейса в наряд; периоды [начало, конец).

FOREIGN KEY (created_by) REFERENCES transport.app_user(id).
FOREIGN KEY (duty_id) REFERENCES transport.duty(id).
FOREIGN KEY (ended_by) REFERENCES transport.app_user(id).
PRIMARY KEY (id).
FOREIGN KEY (trip_id) REFERENCES transport.planned_trip(id).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| id | bigint NN | Идентификатор записи; GENERATED ALWAYS AS IDENTITY |
| trip_id | bigint NN | Плановый рейс |
| duty_id | bigint NN | Ссылка на наряд |
| started_at | timestamp with time zone NN | Начало действия интервала включительно |
| ended_at | timestamp with time zone NULL | Конец действия интервала, не включается |
| created_by | bigint NN | Автор создания |
| reason | text NN | Обязательное содержательное основание |
| ended_by | bigint NULL | Автор прекращения интервала |
| end_reason | text NULL | Основание прекращения интервала |

## connection_state

Последнее подтверждение связи с бортовым устройством, независимо от координаты.

FOREIGN KEY (installation_id) REFERENCES transport.device_installation(id).
PRIMARY KEY (vehicle_id).
FOREIGN KEY (vehicle_id) REFERENCES transport.vehicle(id).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| vehicle_id | bigint NN | Ссылка на транспортное средство |
| last_confirmed_at | timestamp with time zone NULL | Последнее подтверждение связи по БП-08 |
| installation_id | bigint NULL | Ссылка на установку, определяющую принадлежность |
| message_id | uuid NULL | UUID исходного сообщения |
| revision | bigint NN | Положительный номер редакции снимка для копии в Redis; DEFAULT 1 |

## current_position

Последняя пригодная координата транспортного средства.

FOREIGN KEY (installation_id) REFERENCES transport.device_installation(id).
PRIMARY KEY (vehicle_id).
FOREIGN KEY (vehicle_id) REFERENCES transport.vehicle(id).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| vehicle_id | bigint NN | Ссылка на транспортное средство |
| installation_id | bigint NN | Ссылка на установку, определяющую принадлежность |
| message_id | uuid NN | UUID исходного сообщения |
| generated_at | timestamp with time zone NN | Время формирования исходного сообщения |
| latitude | numeric(9,6) NN | Широта, градусы |
| longitude | numeric(9,6) NN | Долгота, градусы |
| speed_kmh | numeric(7,2) NULL | Скорость, км/ч; может отсутствовать |
| revision | bigint NN | Положительный номер редакции снимка для копии в Redis; DEFAULT 1 |

## deviation

Отклонение исполнения от плана.

FOREIGN KEY (closed_by) REFERENCES transport.app_user(id).
FOREIGN KEY (duty_id) REFERENCES transport.duty(id).
PRIMARY KEY (id).
FOREIGN KEY (trip_id) REFERENCES transport.execution(trip_id).
FOREIGN KEY (trip_id, point_no) REFERENCES transport.passage(trip_id, point_no).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| id | bigint NN | Идентификатор записи; GENERATED ALWAYS AS IDENTITY |
| duty_id | bigint NN | Ссылка на наряд |
| trip_id | bigint NULL | Выполнение рейса; пусто для потери связи |
| point_no | integer NULL | Точка прохождения для опоздания и опережения |
| kind | text NN | late — опоздание, early — опережение, off_route — сход с маршрута, connection_lost — потеря связи, failed — невыполнение рейса |
| detected_at | timestamp with time zone NN | Момент обнаружения отклонения |
| measured_value | numeric NULL | Числовая величина; для late/early — минуты |
| episode_started_at | timestamp with time zone NULL | Начало непрерывного периода отсутствия связи |
| closed_at | timestamp with time zone NULL | Момент закрытия отклонения |
| closed_by | bigint NULL | Диспетчер, закрывший отклонение |
| close_reason | text NULL | explained — объяснено, replacement — выполнена подмена, unreliable_data — недостоверные данные |
| comment | text NULL | Пояснение к закрытию |

## device

Зарегистрированное бортовое устройство.

PRIMARY KEY (id).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| id | text NN | Постоянный идентификатор устройства; естественный ключ, не переиспользуется |
| vendor | text NN | Поставщик оборудования |
| message_format | text NN | Код формата сообщения поставщика |
| active | boolean NN | Признак действующей записи; false — выведена из использования; DEFAULT true |

## device_installation

История установки устройства на транспортное средство; периоды [начало, конец).

FOREIGN KEY (created_by) REFERENCES transport.app_user(id).
FOREIGN KEY (device_id) REFERENCES transport.device(id).
FOREIGN KEY (ended_by) REFERENCES transport.app_user(id).
PRIMARY KEY (id).
FOREIGN KEY (vehicle_id) REFERENCES transport.vehicle(id).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| id | bigint NN | Идентификатор записи; GENERATED ALWAYS AS IDENTITY |
| device_id | text NN | Постоянный исходный идентификатор устройства |
| vehicle_id | bigint NN | Ссылка на транспортное средство |
| started_at | timestamp with time zone NN | Начало действия интервала включительно |
| ended_at | timestamp with time zone NULL | Конец действия интервала, не включается |
| created_by | bigint NN | Автор создания |
| reason | text NN | Обязательное содержательное основание |
| ended_by | bigint NULL | Автор прекращения интервала |
| end_reason | text NULL | Основание прекращения интервала |

## driver

Водитель и отметка допуска к выпуску.

UNIQUE (license_no).
PRIMARY KEY (id).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| id | bigint NN | Идентификатор записи; GENERATED ALWAYS AS IDENTITY |
| license_no | text NN | Номер водительского удостоверения |
| full_name | text NN | ФИО сотрудника |
| admitted_until | date NN | Последняя дата действия допуска включительно |
| active | boolean NN | Признак действующей записи; false — выведена из использования; DEFAULT true |

## duty

Наряд: водитель и транспортное средство на набор плановых рейсов одной смены.

FOREIGN KEY (closed_by) REFERENCES transport.app_user(id).
FOREIGN KEY (driver_id) REFERENCES transport.driver(id).
UNIQUE (number).
PRIMARY KEY (id).
FOREIGN KEY (vehicle_id) REFERENCES transport.vehicle(id).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| id | bigint NN | Идентификатор записи; GENERATED ALWAYS AS IDENTITY |
| number | text NN | Номер наряда |
| service_date | date NN | Дата смены в часовом поясе перевозчика |
| vehicle_id | bigint NN | Ссылка на транспортное средство |
| driver_id | bigint NN | Ссылка на водителя |
| status | text NN | planned — запланирован, on_line — на линии, closed — закрыт, cancelled — отменён; DEFAULT 'planned'::text |
| plan_span | tstzrange NULL | Производный интервал назначений [начало, конец) |
| released_at | timestamp with time zone NULL | Момент выпуска наряда |
| closed_at | timestamp with time zone NULL | Момент закрытия или отмены наряда |
| closed_by | bigint NULL | Диспетчер, закрывший или отменивший наряд |
| close_reason | text NULL | Основание закрытия или отмены |
| unchecked_results | boolean NN | Отметка закрытия: на момент закрытия остались выполнения «требует проверки»; последующие решения её не снимают; DEFAULT false |

## execution

Текущее состояние и подтверждённый результат выполнения планового рейса.

PRIMARY KEY (trip_id).
FOREIGN KEY (trip_id) REFERENCES transport.planned_trip(id).
FOREIGN KEY (trip_id, next_point_no) REFERENCES transport.trip_point(trip_id, point_no).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| trip_id | bigint NN | Плановый рейс; одновременно ключ выполнения |
| status | text NN | waiting — ожидает, running — выполняется, review — требует проверки, done — завершён, failed — не выполнен; DEFAULT 'waiting'::text |
| actual_start | timestamp with time zone NULL | Подтверждённое фактическое начало |
| actual_end | timestamp with time zone NULL | Подтверждённое фактическое окончание |
| interrupted_at | timestamp with time zone NULL | Подтверждённый момент прекращения начатого рейса |
| completion_source | text NULL | Источник завершения: telemetry или dispatcher |
| next_point_no | integer NULL | Следующая ожидаемая точка того же рейса |
| review_due_at | timestamp with time zone NN | Следующий срок проверки результата |

## message_key

Реестр исходных ключей сообщений и результата первого получения; хранится 35 суток.

FOREIGN KEY (device_id) REFERENCES transport.device(id).
UNIQUE (first_receipt_id).
FOREIGN KEY (installation_id) REFERENCES transport.device_installation(id).
PRIMARY KEY (device_id, message_id).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| device_id | text NN | Постоянный исходный идентификатор устройства |
| message_id | uuid NN | UUID исходного сообщения |
| content_sha256 | bytea NN | SHA-256 нормализованного содержимого, 32 байта |
| first_received_at | timestamp with time zone NN | Время первого получения; повтор его не продлевает |
| generated_at | timestamp with time zone NULL | Время формирования исходного сообщения |
| installation_id | bigint NULL | Ссылка на установку, определяющую принадлежность |
| first_receipt_id | uuid NN | UUID первого получения в архиве |
| result | text NN | Результат первого получения: accepted, invalid, stale |

## operation_request

Идентификаторы запросов операций: повтор запроса не применяет операцию второй раз.

FOREIGN KEY (actor_id) REFERENCES transport.app_user(id).
PRIMARY KEY (request_id).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| request_id | uuid NN | UUID запроса, присвоенный клиентом до отправки |
| operation | text NN | Имя транзакционной операции |
| arguments | jsonb NN | Параметры операции; повтор с другими параметрами отклоняется |
| actor_id | bigint NN | Пользователь, выполнивший операцию |
| applied_at | timestamp with time zone NN | Время регистрации запроса сервером; DEFAULT CURRENT_TIMESTAMP |

## passage

Зафиксированное прохождение плановой точки.

PRIMARY KEY (trip_id, point_no).
FOREIGN KEY (source_device_id) REFERENCES transport.device(id).
UNIQUE (source_device_id, source_message_id).
FOREIGN KEY (trip_id) REFERENCES transport.execution(trip_id).
FOREIGN KEY (trip_id, point_no) REFERENCES transport.trip_point(trip_id, point_no).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| trip_id | bigint NN | Выполнение рейса (ключ совпадает с плановым рейсом) |
| point_no | integer NN | Номер точки начиная с 1 |
| arrived_at | timestamp with time zone NN | Фактический момент прохождения |
| source_device_id | text NN | Устройство исходного сообщения |
| source_message_id | uuid NN | UUID исходного сообщения, не номера доставки |
| evidence | text NN | Сохранённое основание, доступное после удаления сырого пакета |

## passage_correction

История оценки достоверности прохождения.

FOREIGN KEY (author_id) REFERENCES transport.app_user(id).
PRIMARY KEY (trip_id, point_no, correction_no).
FOREIGN KEY (trip_id, point_no) REFERENCES transport.passage(trip_id, point_no).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| trip_id | bigint NN | Выполнение рейса (ключ совпадает с плановым рейсом) |
| point_no | integer NN | Номер точки начиная с 1 |
| correction_no | integer NN | Порядковый номер исправления данного прохождения |
| corrected_at | timestamp with time zone NN | Время исправления |
| author_id | bigint NN | Пользователь, принявший решение или исправление |
| reliable | boolean NN | Текущая оценка достоверности, задаваемая записью исправления |
| reason | text NN | Обязательное содержательное основание |

## planned_trip

Плановый рейс: рабочая копия плана до первого выпуска, затем неизменный снимок.

PRIMARY KEY (id).
FOREIGN KEY (refreshed_by) REFERENCES transport.app_user(id).
FOREIGN KEY (route_id) REFERENCES transport.route(id).
UNIQUE (route_id, starts_at).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| id | bigint NN | Идентификатор записи; GENERATED ALWAYS AS IDENTITY |
| route_id | bigint NN | Ссылка на направление маршрута |
| starts_at | timestamp with time zone NN | Плановый момент начала рейса |
| path_snapshot | path NN | Сохранённая линия для оценки конкретного рейса |
| copied_at | timestamp with time zone NN | Время первоначального копирования плана; DEFAULT CURRENT_TIMESTAMP |
| refreshed_at | timestamp with time zone NULL | Время явного обновления до выпуска |
| refreshed_by | bigint NULL | Автор явного обновления плана |
| refresh_reason | text NULL | Основание явного обновления |
| frozen_at | timestamp with time zone NULL | Момент первого выпуска и окончательной фиксации плана |

## planning_guard

Служебная строка для последовательного выполнения операций планирования и выпуска.

PRIMARY KEY (id).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| id | integer NN | Идентификатор записи |

## role

Справочник ролей сотрудников.

PRIMARY KEY (code).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| code | text NN | Код роли: admin, dispatcher, driver, analyst |

## route

Маршрут в одном направлении.

UNIQUE (number, direction).
PRIMARY KEY (id).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| id | bigint NN | Идентификатор записи; GENERATED ALWAYS AS IDENTITY |
| number | text NN | Номер маршрута |
| direction | text NN | Направление маршрута |
| name | text NN | Наименование маршрута |
| transport_kind | text NN | Вид транспорта: bus, tram, trolleybus |
| path | path NN | Линия текущего маршрута, (долгота, широта) |
| active | boolean NN | Признак действующей записи; false — выведена из использования; DEFAULT true |

## route_point

Остановка в составе маршрута и плановое смещение от начала рейса.

PRIMARY KEY (route_id, point_no).
FOREIGN KEY (route_id) REFERENCES transport.route(id).
FOREIGN KEY (stop_id) REFERENCES transport.stop(id).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| route_id | bigint NN | Ссылка на направление маршрута |
| point_no | integer NN | Номер точки начиная с 1 |
| stop_id | bigint NN | Ссылка на остановку |
| offset_seconds | integer NN | Смещение от старта в секундах |

## stop

Остановка.

UNIQUE (name, latitude, longitude).
PRIMARY KEY (id).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| id | bigint NN | Идентификатор записи; GENERATED ALWAYS AS IDENTITY |
| name | text NN | Наименование остановки |
| latitude | numeric(9,6) NN | Широта, градусы |
| longitude | numeric(9,6) NN | Долгота, градусы |
| active | boolean NN | Признак действующей записи; false — выведена из использования; DEFAULT true |

## trip_decision

Решение диспетчера по выполнению рейса или попытка проверки.

FOREIGN KEY (author_id) REFERENCES transport.app_user(id).
PRIMARY KEY (trip_id, decision_no).
FOREIGN KEY (trip_id) REFERENCES transport.execution(trip_id).
FOREIGN KEY (trip_id, next_point_no) REFERENCES transport.trip_point(trip_id, point_no).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| trip_id | bigint NN | Выполнение рейса (ключ совпадает с плановым рейсом) |
| decision_no | integer NN | Номер решения внутри выполнения |
| author_id | bigint NN | Пользователь, принявший решение или исправление |
| decided_at | timestamp with time zone NN | Момент регистрации решения |
| outcome | text NN | continue — продолжить, complete — подтвердить завершение, fail — подтвердить невыполнение, insufficient — сведений недостаточно |
| reason | text NN | Обязательное содержательное основание |
| status_before | text NN | Статус выполнения до решения (коды execution.status) |
| status_after | text NN | Статус выполнения после решения (коды execution.status) |
| confirmed_start | timestamp with time zone NULL | Начало, подтверждённое этим решением |
| confirmed_end | timestamp with time zone NULL | Окончание, подтверждённое этим решением |
| interrupted_at | timestamp with time zone NULL | Подтверждённый момент прекращения начатого рейса |
| next_point_no | integer NULL | Следующая ожидаемая точка того же рейса |
| review_due_at | timestamp with time zone NULL | Следующий срок проверки результата |

## trip_point

Плановая точка рейса: сохранённые остановка, координаты и время.

PRIMARY KEY (trip_id, point_no).
FOREIGN KEY (stop_id) REFERENCES transport.stop(id).
FOREIGN KEY (trip_id) REFERENCES transport.planned_trip(id).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| trip_id | bigint NN | Плановый рейс |
| point_no | integer NN | Номер точки начиная с 1 |
| stop_id | bigint NN | Ссылка на остановку |
| arrives_at | timestamp with time zone NN | Плановый момент прибытия |
| stop_name_snapshot | text NN | Название остановки в сохранённом плане |
| latitude_snapshot | numeric(9,6) NN | Широта остановки в сохранённом плане |
| longitude_snapshot | numeric(9,6) NN | Долгота остановки в сохранённом плане |

## user_role

Роли пользователя: связь многие-ко-многим.

PRIMARY KEY (user_id, role_code).
FOREIGN KEY (role_code) REFERENCES transport.role(code).
FOREIGN KEY (user_id) REFERENCES transport.app_user(id).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| user_id | bigint NN | Ссылка на пользователя |
| role_code | text NN | Ссылка на роль |

## vehicle

Транспортное средство, вместимость и отметка допуска.

PRIMARY KEY (id).
UNIQUE (registration_no).

| Атрибут | Тип / обязательность | Значение |
|---|---|---|
| id | bigint NN | Идентификатор записи; GENERATED ALWAYS AS IDENTITY |
| registration_no | text NN | Государственный регистрационный номер |
| transport_kind | text NN | Вид транспорта: bus, tram, trolleybus |
| capacity | integer NN | Вместимость, положительное число мест |
| admitted_until | date NN | Последняя дата действия допуска включительно |
| active | boolean NN | Признак действующей записи; false — выведена из использования; DEFAULT true |

