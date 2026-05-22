package main

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

type DB struct {
	db *sql.DB
}

func NewDB(dsn string) (*DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(3)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	return &DB{db: db}, nil
}

func (d *DB) CreateUser(u *User) error {
	const q = `INSERT INTO users (id, email, name, password_hash) VALUES ($1, $2, $3, $4)`
	_, err := d.db.Exec(q, u.ID, u.Email, u.Name, u.PasswordHash)
	return err
}

func (d *DB) FindUserByEmail(email string) (*User, error) {
	const q = `SELECT id, email, name, password_hash, created_at FROM users WHERE email = $1`
	u := &User{}
	err := d.db.QueryRow(q, email).Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func (d *DB) FindUserByID(id string) (*User, error) {
	const q = `SELECT id, email, name, created_at FROM users WHERE id = $1`
	u := &User{}
	err := d.db.QueryRow(q, id).Scan(&u.ID, &u.Email, &u.Name, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func (d *DB) CreateTeam(t *Team) error {
	const q = `INSERT INTO teams (id, name, plan) VALUES ($1, $2, $3) RETURNING created_at`
	return d.db.QueryRow(q, t.ID, t.Name, t.Plan).Scan(&t.CreatedAt)
}

func (d *DB) FindTeamByID(id string) (*Team, error) {
	const q = `SELECT id, name, plan, created_at FROM teams WHERE id = $1`
	t := &Team{}
	err := d.db.QueryRow(q, id).Scan(&t.ID, &t.Name, &t.Plan, &t.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

func (d *DB) AddMember(teamID, userID, role string) error {
	const q = `
		INSERT INTO team_members (team_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (team_id, user_id) DO UPDATE SET role = EXCLUDED.role`
	_, err := d.db.Exec(q, teamID, userID, role)
	return err
}

func (d *DB) FindMembership(teamID, userID string) (*TeamMember, error) {
	const q = `SELECT team_id, user_id, role, joined_at FROM team_members WHERE team_id = $1 AND user_id = $2`
	m := &TeamMember{}
	err := d.db.QueryRow(q, teamID, userID).Scan(&m.TeamID, &m.UserID, &m.Role, &m.JoinedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return m, err
}

func (d *DB) ListUserTeams(userID string) ([]*TeamMember, error) {
	const q = `SELECT team_id, user_id, role, joined_at FROM team_members WHERE user_id = $1 ORDER BY joined_at`
	rows, err := d.db.Query(q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*TeamMember
	for rows.Next() {
		m := &TeamMember{}
		if err := rows.Scan(&m.TeamID, &m.UserID, &m.Role, &m.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (d *DB) ListTeamMembers(teamID string) ([]*TeamMember, error) {
	const q = `
		SELECT tm.team_id, tm.user_id, u.email, u.name, tm.role, tm.joined_at
		FROM team_members tm
		JOIN users u ON u.id = tm.user_id
		WHERE tm.team_id = $1
		ORDER BY tm.joined_at`
	rows, err := d.db.Query(q, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*TeamMember
	for rows.Next() {
		m := &TeamMember{}
		if err := rows.Scan(&m.TeamID, &m.UserID, &m.Email, &m.Name, &m.Role, &m.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (d *DB) RemoveMember(teamID, userID string) (bool, error) {
	res, err := d.db.Exec(`DELETE FROM team_members WHERE team_id = $1 AND user_id = $2`, teamID, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
