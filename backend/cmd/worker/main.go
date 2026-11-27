package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func run(ctx context.Context, db *pgxpool.Pool) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id,asset_id,next_due,interval_days,checklist FROM maintenance_plans WHERE active AND next_due<=(now() AT TIME ZONE 'Asia/Makassar')::date AND interval_days IS NOT NULL FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return err
	}
	type plan struct {
		id, asset string
		due       time.Time
		days      int
		checklist []byte
	}
	plans := []plan{}
	for rows.Next() {
		var p plan
		if err := rows.Scan(&p.id, &p.asset, &p.due, &p.days, &p.checklist); err != nil {
			rows.Close()
			return err
		}
		plans = append(plans, p)
	}
	rows.Close()
	for _, p := range plans {
		key := p.id + ":" + p.due.Format("2006-01-02")
		_, err = tx.Exec(ctx, `INSERT INTO work_orders(asset_id,plan_id,occurrence_key,status,due_on,checklist) VALUES($1,$2,$3,'terjadwal',$4,$5) ON CONFLICT(occurrence_key) DO NOTHING`, p.asset, p.id, key, p.due, p.checklist)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `WITH created AS (INSERT INTO notifications(recipient_id,event_key,title,body,entity_type,entity_id) SELECT u.id,$1,'Preventive maintenance jatuh tempo','Work order terjadwal untuk aset simulasi','asset',$2 FROM users u WHERE u.role IN ('admin','staff') ON CONFLICT(recipient_id,event_key) DO NOTHING RETURNING id) INSERT INTO email_deliveries(notification_id) SELECT id FROM created ON CONFLICT DO NOTHING`, "preventive:"+key, p.asset)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE maintenance_plans SET next_due=next_due+$2::integer WHERE id=$1`, p.id, p.days)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func sendPending(ctx context.Context, db *pgxpool.Pool) error {
	host := os.Getenv("SMTP_HOST")
	port := os.Getenv("SMTP_PORT")
	allow := strings.Split(strings.ToLower(os.Getenv("DEMO_EMAIL_ALLOWLIST")), ",")
	if host == "" || port == "" || len(allow) == 0 || allow[0] == "" {
		return nil
	}
	rows, err := db.Query(ctx, `SELECT d.id,u.email,n.title,n.body FROM email_deliveries d JOIN notifications n ON n.id=d.notification_id JOIN users u ON u.id=n.recipient_id WHERE d.status='pending' AND d.next_attempt_at<=now() ORDER BY d.next_attempt_at LIMIT 20`)
	if err != nil {
		return err
	}
	type email struct{ id, address, title, body string }
	items := []email{}
	for rows.Next() {
		var e email
		if err = rows.Scan(&e.id, &e.address, &e.title, &e.body); err != nil {
			rows.Close()
			return err
		}
		items = append(items, e)
	}
	rows.Close()
	for _, e := range items {
		allowed := false
		for _, a := range allow {
			if strings.TrimSpace(a) == strings.ToLower(e.address) {
				allowed = true
			}
		}
		if !allowed {
			_, err = db.Exec(ctx, `UPDATE email_deliveries SET status='skipped',last_error='recipient not in demo allowlist' WHERE id=$1`, e.id)
			if err != nil {
				return err
			}
			continue
		}
		from := os.Getenv("SMTP_FROM")
		if from == "" {
			from = "wbl-demo@localhost"
		}
		message := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", from, e.address, e.title, e.body)
		var auth smtp.Auth
		if user := os.Getenv("SMTP_USER"); user != "" {
			auth = smtp.PlainAuth("", user, os.Getenv("SMTP_PASSWORD"), host)
		}
		sendErr := smtp.SendMail(net.JoinHostPort(host, port), auth, from, []string{e.address}, []byte(message))
		if sendErr != nil {
			_, err = db.Exec(ctx, `UPDATE email_deliveries SET attempts=attempts+1,last_error=$2,next_attempt_at=now()+interval '1 minute'*power(2,least(attempts,6)),status=CASE WHEN attempts>=4 THEN 'failed' ELSE 'pending' END WHERE id=$1`, e.id, sendErr.Error())
		} else {
			_, err = db.Exec(ctx, `UPDATE email_deliveries SET attempts=attempts+1,status='sent',last_error=NULL WHERE id=$1`, e.id)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
func main() {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		log.Fatal("DATABASE_URL required")
	}
	db, err := pgxpool.New(context.Background(), url)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = run(ctx, db)
		if err == nil {
			err = sendPending(ctx, db)
		}
		cancel()
		if err != nil {
			log.Printf("worker: %v", err)
		}
		time.Sleep(time.Minute)
	}
}
