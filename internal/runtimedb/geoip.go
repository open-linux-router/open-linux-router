package runtimedb

import "database/sql"

type GeoIP struct {
	IP            string
	Data          []byte
	Fetched, Used int64
}

func (s *Store) GeoIPs() ([]GeoIP, error) {
	rows, err := s.db.Query("SELECT ip,data,fetched,used FROM geoip_cache ORDER BY used DESC LIMIT 2000")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GeoIP
	for rows.Next() {
		var r GeoIP
		if err := rows.Scan(&r.IP, &r.Data, &r.Fetched, &r.Used); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) SaveGeoIP(r GeoIP) error {
	return s.transaction(func(tx *sql.Tx) error {
		if _, err := tx.Exec("INSERT OR REPLACE INTO geoip_cache(ip,data,fetched,used) VALUES(?,?,?,?)", r.IP, r.Data, r.Fetched, r.Used); err != nil {
			return err
		}
		_, err := tx.Exec("DELETE FROM geoip_cache WHERE ip IN (SELECT ip FROM geoip_cache ORDER BY used DESC,ip LIMIT -1 OFFSET 2000)")
		return err
	})
}

func (s *Store) TouchGeoIP(ip string, used int64) error {
	_, err := s.db.Exec("UPDATE geoip_cache SET used=? WHERE ip=?", used, ip)
	return err
}
