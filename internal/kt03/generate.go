package kt03

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"time"
)

const null = `\N`
const generatorVersion = 2

type tableFile struct {
	Table   string `json:"table"`
	Columns string `json:"columns"`
	Rows    int    `json:"rows"`
	SHA256  string `json:"sha256"`
	file    *os.File
	csv     *csv.Writer
	hash    hash.Hash
}
type manifest struct {
	Version  int          `json:"generator_version"`
	Seed     int64        `json:"seed"`
	Mode     string       `json:"mode"`
	Anchor   string       `json:"anchor"`
	Days     int          `json:"days"`
	Vehicles int          `json:"vehicles"`
	Tables   []*tableFile `json:"tables"`
}

func uuid(seed int64, kind string, n int) string {
	b := sha256.Sum256([]byte(fmt.Sprintf("%d/%s/%d", seed, kind, n)))
	b[6] = (b[6] & 15) | 0x40
	b[8] = (b[8] & 63) | 0x80
	s := hex.EncodeToString(b[:16])
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}
func stamp(t time.Time) string { return t.Format(time.RFC3339) }
func (t *tableFile) row(v ...any) {
	a := make([]string, len(v))
	for i, x := range v {
		a[i] = fmt.Sprint(x)
	}
	if t.csv.Error() == nil {
		_ = t.csv.Write(a)
		t.Rows++
	}
}

func (r *runner) generate() error {
	if err := os.MkdirAll(r.data, 0755); err != nil {
		return err
	}
	rng := rand.New(rand.NewSource(r.seed))
	days, vehicles, n := 7, 20, 75000
	if r.mode == "load" {
		days, vehicles, n = 30, 100, 3000000
	}
	if r.rows > 0 {
		n = r.rows
	}
	anchor := time.Date(2026, 10, 11, 13, 30, 0, 0, time.FixedZone("Asia/Novosibirsk", 7*3600))
	base := time.Date(2026, 10, 11, 0, 0, 0, 0, anchor.Location()).AddDate(0, 0, 1-days)
	m := manifest{Version: generatorVersion, Seed: r.seed, Mode: r.mode, Anchor: stamp(anchor), Days: days, Vehicles: vehicles}
	definitions := [][2]string{
		{"driver", "id,license_no,full_name,admitted_until"},
		{"app_user", "id,login,full_name"}, {"user_role", "user_id,role_code"},
		{"stop", "id,name,latitude,longitude"}, {"route", "id,number,direction,name,transport_kind,path"},
		{"route_point", "route_id,point_no,stop_id,offset_seconds"},
		{"vehicle", "id,registration_no,transport_kind,capacity,admitted_until"},
		{"device", "id,vendor,message_format"},
		{"device_installation", "id,device_id,vehicle_id,started_at,created_by,reason"},
		{"planned_trip", "id,route_id,starts_at,path_snapshot,copied_at"},
		{"trip_point", "trip_id,point_no,stop_id,arrives_at,stop_name_snapshot,latitude_snapshot,longitude_snapshot"},
		{"duty", "id,number,service_date,vehicle_id,driver_id,status,released_at,closed_at,closed_by,close_reason,unchecked_results"},
		{"assignment", "id,trip_id,duty_id,started_at,created_by,reason"},
		{"execution", "trip_id,status,actual_start,actual_end,completion_source,next_point_no,review_due_at"},
		{"trip_decision", "trip_id,decision_no,author_id,decided_at,outcome,reason,status_before,status_after,confirmed_start,confirmed_end"},
		{"passage", "trip_id,point_no,arrived_at,source_device_id,source_message_id,evidence"},
		{"passage_correction", "trip_id,point_no,correction_no,corrected_at,author_id,reliable,reason"},
		{"deviation", "id,duty_id,trip_id,point_no,kind,detected_at,measured_value,closed_at,closed_by,close_reason,comment"},
		{"message_key", "device_id,message_id,content_sha256,first_received_at,generated_at,installation_id,first_receipt_id,result"},
	}
	tables := map[string]*tableFile{}
	for _, d := range definitions {
		f, err := os.Create(filepath.Join(r.data, d[0]+".csv"))
		if err != nil {
			return err
		}
		defer f.Close()
		h := sha256.New()
		t := &tableFile{Table: d[0], Columns: d[1], file: f, hash: h, csv: csv.NewWriter(io.MultiWriter(f, h))}
		tables[d[0]] = t
		m.Tables = append(m.Tables, t)
	}
	for i, role := range []string{"admin", "dispatcher", "analyst"} {
		tables["app_user"].row(i+1, role, role)
		tables["user_role"].row(i+1, role)
	}
	for i := 1; i <= 20; i++ {
		tables["stop"].row(i, fmt.Sprintf("Остановка %02d", i), fmt.Sprintf("%.6f", 55+float64(i)/1000), fmt.Sprintf("%.6f", 82.9+float64(i)/1000))
	}
	path := "[(82.901,55.001),(82.920,55.020)]"
	for i := 1; i <= 12; i++ {
		tables["route"].row(i, fmt.Sprintf("М-%02d", i), "прямое", fmt.Sprintf("Учебный маршрут %d", i), "bus", path)
		for p := 1; p <= 20; p++ {
			tables["route_point"].row(i, p, p, (p-1)*120)
		}
	}
	for v := 1; v <= vehicles; v++ {
		device := fmt.Sprintf("device-%03d", v)
		tables["driver"].row(v, fmt.Sprintf("LIC-%03d", v), fmt.Sprintf("Водитель %03d", v), "2027-12-31")
		tables["vehicle"].row(v, fmt.Sprintf("BUS-%03d", v), "bus", 40+20*(v%3), "2027-12-31")
		tables["device"].row(device, fmt.Sprintf("vendor-%d", v%2+1), fmt.Sprintf("format-%d", v%2+1))
		tables["device_installation"].row(v, device, v, stamp(base.AddDate(0, 0, -1)), 1, "Учебная установка")
	}
	trip, assignment, deviation := 0, 0, 0
	for d := 0; d < days; d++ {
		for v := 1; v <= vehicles; v++ {
			day := base.AddDate(0, 0, d)
			duty := d*vehicles + v
			state := "closed"
			if d == days-1 {
				state = "on_line"
				if v%10 == 0 {
					state = "planned"
				}
				if v%10 == 1 {
					state = "cancelled"
				}
			}
			released, closed, closedBy, reason := stamp(day.Add(6*time.Hour)), stamp(day.Add(21*time.Hour)), "2", "Смена завершена"
			if state == "planned" {
				released, closed, closedBy, reason = null, null, null, null
			}
			if state == "on_line" {
				closed, closedBy, reason = null, null, null
			}
			if state == "cancelled" {
				released = null
				closed = stamp(day.Add(5 * time.Hour))
				reason = "Резерв не потребовался"
			}
			route := 1 + rng.Intn(2)
			if rng.Intn(100) >= 70 {
				route = 3 + rng.Intn(10)
			}
			hasReview := false
			for slot := 0; slot < 4; slot++ {
				trip++
				start := day.Add(time.Duration(7+slot*3)*time.Hour + time.Duration(v*5)*time.Second)
				end := start.Add(38 * time.Minute)
				copied := stamp(day.Add(-12 * time.Hour))
				tables["planned_trip"].row(trip, route, stamp(start), path, copied)
				for p := 1; p <= 20; p++ {
					tables["trip_point"].row(trip, p, p, stamp(start.Add(time.Duration((p-1)*120)*time.Second)), fmt.Sprintf("Остановка %02d", p), fmt.Sprintf("%.6f", 55+float64(p)/1000), fmt.Sprintf("%.6f", 82.9+float64(p)/1000))
				}
				if state != "cancelled" {
					assignment++
					tables["assignment"].row(assignment, trip, duty, copied, 1, "План смены")
				}
				if state == "planned" || state == "cancelled" {
					continue
				}
				roll := rng.Intn(100)
				status, source := "done", "telemetry"
				if roll >= 75 && roll < 85 {
					status = "failed"
				}
				if roll >= 85 && roll < 95 {
					status = "review"
				}
				if roll >= 95 {
					source = "dispatcher"
				}
				if state == "on_line" {
					status = "done"
					source = "telemetry"
					if slot == 2 {
						status = "running"
						if v%2 == 0 {
							status = "review"
						}
					}
					if slot == 3 {
						status = "waiting"
					}
				}
				if hasReview {
					status = "waiting"
					if !start.Add(15 * time.Minute).After(anchor) {
						status = "review"
					}
				}
				if status == "review" {
					hasReview = true
				}
				delay := rng.Intn(5)
				if rng.Intn(100) < 20 {
					delay = 6 + rng.Intn(10)
				} else if rng.Intn(100) < 5 {
					delay = -3
				}
				if status == "running" {
					delay = 0
				}
				startDelay := delay
				if startDelay < -2 {
					startDelay = -2
				}
				actualStart, actualEnd := start.Add(time.Duration(startDelay)*time.Minute), end.Add(time.Duration(delay)*time.Minute)
				a, b, src, next := null, null, null, null
				if status == "done" {
					a, b, src = stamp(actualStart), stamp(actualEnd), source
				}
				if status == "running" {
					a, next = stamp(actualStart), "10"
				}
				tables["execution"].row(trip, status, a, b, src, next, stamp(end.Add(15*time.Minute)))
				if status == "failed" {
					when := stamp(start.Add(10 * time.Minute))
					tables["trip_decision"].row(trip, 1, 2, when, "fail", "Подтверждён невыезд", "waiting", "failed", null, null)
					deviation++
					tables["deviation"].row(deviation, duty, trip, null, "failed", when, null, null, null, null, null)
				}
				if status == "done" && source == "dispatcher" {
					tables["trip_decision"].row(trip, 1, 2, stamp(actualEnd.Add(5*time.Minute)), "complete", "Подтверждение водителя", "review", "done", a, b)
				}
				points := 0
				if status == "done" && source == "telemetry" {
					points = 20
				}
				if status == "running" {
					points = 9
				}
				for p := 1; p <= points; p++ {
					pointDelay := delay
					if p == 1 {
						pointDelay = startDelay
					}
					arrived := start.Add(time.Duration((p-1)*120+pointDelay*60) * time.Second)
					message := uuid(r.seed, "passage", trip*20+p)
					device := fmt.Sprintf("device-%03d", v)
					tables["passage"].row(trip, p, stamp(arrived), device, message, "Синтетическая координата в контрольной точке")
					sum := sha256.Sum256([]byte(fmt.Sprintf("%d/passage/%d/%d", r.seed, trip, p)))
					tables["message_key"].row(device, message, `\x`+hex.EncodeToString(sum[:]), stamp(arrived.Add(time.Second)), stamp(arrived), v, uuid(r.seed, "passage-receipt", trip*20+p), "accepted")
					if pointDelay >= 6 || pointDelay <= -3 {
						kind := "late"
						if pointDelay < 0 {
							kind = "early"
						}
						cl, by, why, note := null, null, null, null
						if rng.Intn(100) < 70 {
							cl, by, why, note = stamp(arrived.Add(time.Minute)), "2", "explained", "Диспетчер проверил событие"
						}
						if cl != null && rng.Intn(100) < 3 {
							why = "unreliable_data"
							note = "Ошибка координаты"
							tables["passage_correction"].row(trip, p, 1, cl, 2, false, note)
						}
						deviation++
						tables["deviation"].row(deviation, duty, trip, p, kind, stamp(arrived), pointDelay, cl, by, why, note)
					}
				}
			}
			tables["duty"].row(duty, fmt.Sprintf("Н-%05d", duty), day.Format("2006-01-02"), v, v, state, released, closed, closedBy, reason, state == "closed" && hasReview)
		}
	}
	required := tables["message_key"].Rows
	remaining := n - required
	acceptedRemaining := n*92/100 - required
	if acceptedRemaining < 0 {
		return fmt.Errorf("--rows=%d is too small for %d passage messages at 92%% accepted", n, required)
	}
	for i := 1; i <= remaining; i++ {
		if i%10000 == 0 {
			if err := r.ctx.Err(); err != nil {
				return err
			}
		}
		v := 1 + rng.Intn(vehicles)
		if rng.Intn(100) < 70 {
			v = 1 + rng.Intn(vehicles/10)
		}
		d := rng.Intn(days)
		hour := rng.Intn(24)
		if rng.Intn(100) < 70 {
			hour = 7 + rng.Intn(3)
			if rng.Intn(2) == 0 {
				hour = 16 + rng.Intn(3)
			}
		}
		if d == days-1 {
			hour = 6 + rng.Intn(7)
		}
		received := base.AddDate(0, 0, d).Add(time.Duration(hour)*time.Hour + time.Duration(rng.Intn(3600))*time.Second)
		generated := received.Add(-time.Duration(rng.Intn(30)) * time.Second)
		result := "accepted"
		roll := rng.Intn(remaining)
		if roll >= acceptedRemaining {
			result = "invalid"
		}
		if roll >= acceptedRemaining+n*5/100 {
			result = "stale"
			generated = received.Add(-20 * time.Minute)
		}
		gen, installation := stamp(generated), fmt.Sprint(v)
		if result == "invalid" {
			gen, installation = null, null
		}
		sum := sha256.Sum256([]byte(fmt.Sprintf("%d/payload/%d", r.seed, i)))
		tables["message_key"].row(fmt.Sprintf("device-%03d", v), uuid(r.seed, "message", i), `\x`+hex.EncodeToString(sum[:]), stamp(received), gen, installation, uuid(r.seed, "receipt", i), result)
	}
	for _, t := range m.Tables {
		t.csv.Flush()
		if err := t.csv.Error(); err != nil {
			return err
		}
		if err := t.file.Close(); err != nil {
			return err
		}
		t.SHA256 = hex.EncodeToString(t.hash.Sum(nil))
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(r.data, "manifest.json"), append(b, '\n'), 0644); err != nil {
		return err
	}
	fmt.Printf("Generated %s seed=%d messages=%d trips=%d in %s\n", r.mode, r.seed, n, trip, r.data)
	return r.save("manifest-"+r.mode+".json", m)
}
