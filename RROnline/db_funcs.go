package RROnline

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"sync"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"../Reddit-Refresh-Go/reddit_refresh_go/reddit_refresh"
)

/*
Device represents a single user device in the database
*/
type Device struct {
	Email    string `db:"email"`
	DeviceID string `db:"device_id"`
	Nickname string `db:"nickname"`
	Active   bool   `db:"active"`
}

/*
Search represents a single user search in the database
*/
type Search struct {
	Email      string `db:"email"`
	Sub        string `db:"sub"`
	Search     string `db:"search"`
	LastResult string `db:"last_result"`
}

/*
UserInfo represents a single user in the database
*/
type UserInfo struct {
	Email    string  `db:"email"`
	Interval float32 `db:"interval_min"`
	Token    string  `db:"access_token"`
}

const passwdFile = "username"
const userFile = "password"
const connStr = "postgres://%s:%s@traphouse.us/reddit_refresh_online"

//DefaultInterval is the default interval for a user when first created
const DefaultInterval = 10

const searchQueryAllStr = "SELECT email, sub, search, last_result FROM search"
const searchQueryStr = "SELECT email, sub, search, last_result " +
	"FROM search WHERE email = $1 ORDER BY create_time"
const searchIndQueryStr = "SELECT email, sub, search, last_result " +
	"FROM search WHERE email = $1 and sub = $2 and search = $3"
const searchDelStr = "DELETE FROM search " +
	"WHERE email = ? AND sub = ? AND search NOT IN (?)"
const searchDelSubStr = "DELETE FROM search " +
	"WHERE email = $1 AND sub = $2"
const searchInsStr = "INSERT INTO search (email, sub, search, last_result)" +
	"	VALUES ($1, $2, $3, $4)"
const searchUpdStr = "UPDATE search SET last_result = $1" +
	"	WHERE email = $2 AND sub = $3 AND search = $4"
const dupSearchErr = "pq: duplicate key value violates unique constraint \"search_pk\""

const userInfoQueryStr = "SELECT email, interval_min, access_token FROM user_info" +
	"	WHERE email = $1"
const userInfoInsStr = "INSERT INTO user_info (email, interval_min, access_token)" +
	"	VALUES ($1, $2)"
const userInfoUpdStr = "UPDATE user_info SET access_token = $1" +
	"	WHERE email = $2"

const devicesInsStr = "INSERT INTO device (email, device_id, nickname)" +
	"	VALUES ($1, $2, $3)"
const devicesQueryStr = "SELECT email, device_id, nickname, active" +
	"	FROM device WHERE email = $1"
const devicesDelStr = "DELETE FROM device WHERE device_id = $1"
const devicesDelAllStr = "DELETE FROM device WHERE email = $1"

/*
Connect returns a sqlx database connection for the database
*/
func Connect() *sqlx.DB {
	username, _ := ioutil.ReadFile(userFile)
	password, _ := ioutil.ReadFile(passwdFile)
	_ = pq.Efatal //weird fix for bug with pq
	fullConStr := fmt.Sprintf(connStr, string(password), string(username))
	db, err := sqlx.Open("postgres", fullConStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to PGSQL DB.\n")
	}
	return db
}

/*
RefreshDevices refreshes the user's devices in the database
token is the user's Pushbullet API token
db is the database to use for connection, or nil
*/
func RefreshDevices(token string, db *sqlx.DB) map[string]string {
	if db == nil {
		db = Connect()
	}
	email := GetEmail(token)
	devices := reddit_refresh.GetDevices(token)
	_, err := db.Exec(devicesDelAllStr, email)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting devices for %s", email)
	}
	var wg sync.WaitGroup
	wg.Add(len(devices))
	fmt.Println(devices)
	for nickname, iden := range devices {
		go AddDevice(email, iden, nickname, db, &wg)
	}
	//wait for all AddDevice calls to exit
	wg.Wait()
	return devices
}

/*
GetDevices gets all devices in the DB for a given user
db is the database to use for connection, or nil
*/
func GetDevices(email string, db *sqlx.DB) []Device {
	if db == nil {
		db = Connect()
	}
	devices := []Device{}
	err := db.Select(&devices, devicesQueryStr, email)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting devices for %s\n", email)
	}
	return devices
}

/*
AddDevice adds a given device to the database for the specified user
deviceID is the UUID for the device given by Pushbullet
nickname is the nickname that Pushbullet gives
db can be a sqlx DB connection or nil
wg is the WaitGroup to use when this is a goroutine
*/
func AddDevice(email string, deviceID string, nickname string, db *sqlx.DB, wg *sync.WaitGroup) {
	//wait until end of function to tell wait group that we're exiting
	defer wg.Done()
	if db == nil {
		db = Connect()
	}
	_, err := db.Exec(devicesInsStr, email, deviceID, nickname)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error inserting device %s for %s\n",
			deviceID, email)
		fmt.Println(err)
	}
}

/*
DeleteDevice deletes a given device from the database
deviceID is the UUID for the device
NOTE: we don't need the email since each device ID is unique
*/
func DeleteDevice(deviceID string) {
	db := Connect()
	_, err := db.Exec(devicesDelStr, deviceID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting device %s\n", deviceID)
	}
}

/*
GetAllSearches gets every search from the database, used at program startup
*/
func GetAllSearches() []Search {
	db := Connect()
	searches := []Search{}
	err := db.Select(&searches, searchQueryAllStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting all searches\n")
	}
	return searches
}

/*
GetSearches gets all the searches in the DB for the given user
db is the database to use for connection, or nil
*/
func GetSearches(email string, db *sqlx.DB) []Search {
	if db == nil {
		db = Connect()
	}
	searches := []Search{}
	err := db.Select(&searches, searchQueryStr, email)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting searches for %s\n", email)
	}
	return searches
}

/*
GetLastRes gets the last search result for the given search and user
sub is the subreddit that the search is for
*/
func GetLastRes(email string, sub string, search string) string {
	db := Connect()
	defer db.Close()
	searches := []Search{}
	err := db.Select(&searches, searchIndQueryStr, email, sub, search)
	if err != nil || len(searches) == 0 {
		fmt.Fprintf(os.Stderr, fmt.Sprintf("Error getting search (%s, %s, %s)",
			email, sub, search))
	}
	return searches[0].LastResult
}

/*
DeleteMissingSearches deletes all searchs in the DB
not in the searches slice
searches is a slice containing the new searches
*/
func DeleteMissingSearches(email string, sub string, searches []string) error {
	db := Connect()
	query, args, err := sqlx.In(searchDelStr, email, sub, searches)
	query = sqlx.Rebind(sqlx.DOLLAR, query)
	result, err := db.Exec(query, args...)
	fmt.Println(result.RowsAffected())
	// TODO: figure out how to delete old search goroutines
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting old searches for (%s, %s)\n",
			email, sub)
		fmt.Println(err)
		return errors.New("Could not delete old searches")
	}
	return nil
}

/*
DeleteSub deletes all the searches in the database for a given user and subreddit
*/
func DeleteSub(email string, sub string) error {
	db := Connect()
	_, err := db.Exec(searchDelSubStr, email, sub)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting sub (%s, %s)\n",
			email, sub)
		return errors.New("Could not delete sub")
	}
	// TODO: add code to delete all sub goroutines
	return nil
}

/*
AddSearch adds a single search to the DB for a given user and subreddit
*/
func AddSearch(email string, sub string, search string) {
	db := Connect()
	_, err := db.Exec(searchInsStr, email, sub, search, "")
	if err != nil {
		if err.Error() != dupSearchErr {
			fmt.Fprintf(os.Stderr, "Error inserting search for %s\n", email)
			fmt.Println(err)
		}
	}
	//TODO: add code to start a search goroutine
}

/*
UserExists checks whether or not a user exists in the database
*/
func UserExists(email string) bool {
	db := Connect()
	users := []UserInfo{}
	err := db.Select(&users, userInfoQueryStr, email)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting interval for %s\n", email)
	}
	return len(users) != 0
}

/*
GetInterval gets the refresh interval for a given user
*/
func GetInterval(email string) float32 {
	db := Connect()
	users := []UserInfo{}
	err := db.Select(&users, userInfoQueryStr, email)
	if err != nil || len(users) == 0 {
		fmt.Fprintf(os.Stderr, "Error getting interval for %s\n", email)
	}
	return users[0].Interval
}

/*
GetUserToken gets a user's Pushbullet access token from the database,
mostly used for starting threads when the application reboots
*/
func GetUserToken(email string) string {
	db := Connect()
	users := []UserInfo{}
	err := db.Select(&users, userInfoQueryStr, email)
	if err != nil || len(users) == 0 {
		fmt.Fprintf(os.Stderr, "Error getting token for %s\n", email)
	}
	return users[0].Token
}

/*
UpdateUserToken updates a user's access token in the database
*/
func UpdateUserToken(email string, token string) {
	db := Connect()
	_, err := db.Exec(userInfoUpdStr, token, email)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error updating %s to %s\n", email, token)
	}
}

/*
UpdateLastRes updates the last result URL for a search for a given user
*/
func UpdateLastRes(email string, sub string, search string, url string) {
	db := Connect()
	_, err := db.Exec(searchUpdStr, url, email, sub, search)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error updating (%s, %s, %s) to %s\n",
			email, sub, search, url)
	}
}

/*
AddUser adds a user to the database given their interval and token
*/
func AddUser(email string, interval float32, token string, db *sqlx.DB) {
	if db == nil {
		db = Connect()
	}
	_, err := db.Exec(userInfoInsStr, email, interval, token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating user %s\n", email)
	}
}
