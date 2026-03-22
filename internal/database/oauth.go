package database

type OAuthAccount struct {
	Provider   string `json:"provider"`
	ProviderID string `json:"provider_id"`
	UserID     string `json:"user_id"`
	Username   string `json:"username"`
	AvatarURL  string `json:"avatar_url"`
}

func (db *DB) GetOAuthAccount(provider, providerID string) (*OAuthAccount, error) {
	a := &OAuthAccount{}
	err := db.QueryRow(
		`SELECT provider, provider_id, user_id, username, avatar_url
		 FROM oauth_accounts WHERE provider = $1 AND provider_id = $2`,
		provider, providerID,
	).Scan(&a.Provider, &a.ProviderID, &a.UserID, &a.Username, &a.AvatarURL)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (db *DB) CreateOAuthAccount(a *OAuthAccount) error {
	_, err := db.Exec(
		`INSERT INTO oauth_accounts (provider, provider_id, user_id, username, avatar_url)
		 VALUES ($1, $2, $3, $4, $5)`,
		a.Provider, a.ProviderID, a.UserID, a.Username, a.AvatarURL,
	)
	return err
}

func (db *DB) DeleteOAuthAccount(provider, providerID string) error {
	_, err := db.Exec("DELETE FROM oauth_accounts WHERE provider = $1 AND provider_id = $2", provider, providerID)
	return err
}

func (db *DB) ListOAuthAccountsByUser(userID string) ([]OAuthAccount, error) {
	rows, err := db.Query(
		`SELECT provider, provider_id, user_id, username, avatar_url
		 FROM oauth_accounts WHERE user_id = $1`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []OAuthAccount
	for rows.Next() {
		var a OAuthAccount
		if err := rows.Scan(&a.Provider, &a.ProviderID, &a.UserID, &a.Username, &a.AvatarURL); err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}
