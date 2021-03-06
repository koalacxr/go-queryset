package queryset

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/mysql"
	"github.com/jirfag/go-queryset/queryset/test"
	"github.com/stretchr/testify/assert"

	"gopkg.in/DATA-DOG/go-sqlmock.v1"
)

func fixedFullRe(s string) string {
	return fmt.Sprintf("^%s$", regexp.QuoteMeta(s))
}

func newDB() (sqlmock.Sqlmock, *gorm.DB) {
	db, mock, err := sqlmock.New()
	if err != nil {
		log.Fatalf("can't create sqlmock: %s", err)
	}

	gormDB, gerr := gorm.Open("mysql", db)
	if gerr != nil {
		log.Fatalf("can't open gorm connection: %s", err)
	}
	gormDB.LogMode(true)

	return mock, gormDB.Set("gorm:update_column", true)
}

func getRowsForUsers(users []test.User) *sqlmock.Rows {
	var userFieldNames = []string{"id", "name", "email", "created_at", "updated_at", "deleted_at"}
	rows := sqlmock.NewRows(userFieldNames)
	for _, u := range users {
		rows = rows.AddRow(u.ID, u.Name, u.Email, u.CreatedAt, u.UpdatedAt, u.DeletedAt)
	}
	return rows
}

func getTestUsers(n int) (ret []test.User) {
	for i := 0; i < n; i++ {
		u := test.User{
			Model: gorm.Model{
				ID:        uint(i),
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			Email: fmt.Sprintf("u%d@mail.ru", i),
			Name:  fmt.Sprintf("name_%d", i),
		}
		ret = append(ret, u)
	}
	return
}

func getUserNoID() test.User {
	return test.User{
		Model: gorm.Model{
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		Email: "qs@mail.ru",
		Name:  fmt.Sprintf("name_rand_%d", rand.Int()),
	}
}

func getUser() test.User {
	u := getUserNoID()
	u.ID = uint(rand.Int())
	return u
}

func checkMock(t *testing.T, mock sqlmock.Sqlmock) {
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expections: %s", err)
	}
}

type testQueryFunc func(t *testing.T, m sqlmock.Sqlmock, db *gorm.DB)

func TestQueries(t *testing.T) {
	funcs := []testQueryFunc{
		testUserSelectAll,
		testUserSelectAllNoRecords,
		testUserSelectOne,
		testUserCreateOne,
		testUserUpdateFieldsByPK,
		testUserUpdateByEmail,
		testUserDeleteByEmail,
		testUserDeleteByPK,
	}
	for _, f := range funcs {
		f := f // save range var
		funcName := runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
		funcName = filepath.Ext(funcName)
		funcName = strings.TrimPrefix(funcName, ".")
		t.Run(funcName, func(t *testing.T) {
			t.Parallel()
			m, db := newDB()
			defer checkMock(t, m)
			f(t, m, db)
		})
	}
}

func testUserSelectAll(t *testing.T, m sqlmock.Sqlmock, db *gorm.DB) {
	expUsers := getTestUsers(2)
	m.ExpectQuery(fixedFullRe("SELECT * FROM `users` WHERE `users`.deleted_at IS NULL")).
		WillReturnRows(getRowsForUsers(expUsers))

	var users []test.User
	assert.Nil(t, test.NewUserQuerySet(db).All(&users))
	assert.Equal(t, expUsers, users)
}

func testUserSelectAllNoRecords(t *testing.T, m sqlmock.Sqlmock, db *gorm.DB) {
	m.ExpectQuery(fixedFullRe("SELECT * FROM `users` WHERE `users`.deleted_at IS NULL")).
		WillReturnError(sql.ErrNoRows)

	var users []test.User
	assert.Error(t, gorm.ErrRecordNotFound, test.NewUserQuerySet(db).All(&users))
	assert.Len(t, users, 0)
}

func testUserSelectOne(t *testing.T, m sqlmock.Sqlmock, db *gorm.DB) {
	expUsers := getTestUsers(1)
	req := "SELECT * FROM `users` WHERE `users`.deleted_at IS NULL ORDER BY `users`.`id` ASC LIMIT 1"
	m.ExpectQuery(fixedFullRe(req)).
		WillReturnRows(getRowsForUsers(expUsers))

	var user test.User
	assert.Nil(t, test.NewUserQuerySet(db).One(&user))
	assert.Equal(t, expUsers[0], user)
}

func testUserCreateOne(t *testing.T, m sqlmock.Sqlmock, db *gorm.DB) {
	u := getUserNoID()
	req := "INSERT INTO `users` (`created_at`,`updated_at`,`deleted_at`,`name`,`email`) VALUES (?,?,?,?,?)"
	args := []driver.Value{sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
		u.Name, u.Email}
	m.ExpectExec(fixedFullRe(req)).
		WithArgs(args...).
		WillReturnResult(sqlmock.NewResult(2, 1))
	assert.Nil(t, u.Create(db))
	assert.Equal(t, uint(2), u.ID)
}

func testUserUpdateByEmail(t *testing.T, m sqlmock.Sqlmock, db *gorm.DB) {
	u := getUser()
	req := "UPDATE `users` SET `name` = ? WHERE `users`.deleted_at IS NULL AND ((email = ?))"
	m.ExpectExec(fixedFullRe(req)).
		WithArgs(u.Name, u.Email).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := test.NewUserQuerySet(db).
		EmailEq(u.Email).
		GetUpdater().
		SetName(u.Name).
		Update()
	assert.Nil(t, err)
}

func testUserUpdateFieldsByPK(t *testing.T, m sqlmock.Sqlmock, db *gorm.DB) {
	u := getUser()
	req := "UPDATE `users` SET `name` = ? WHERE `users`.deleted_at IS NULL AND `users`.`id` = ?"
	m.ExpectExec(fixedFullRe(req)).
		WithArgs(u.Name, u.ID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	assert.Nil(t, u.Update(db, test.UserDBSchema.Name))
}

func testUserDeleteByEmail(t *testing.T, m sqlmock.Sqlmock, db *gorm.DB) {
	u := getUser()
	req := "UPDATE `users` SET deleted_at=? WHERE `users`.deleted_at IS NULL AND ((email = ?))"
	m.ExpectExec(fixedFullRe(req)).
		WithArgs(sqlmock.AnyArg(), u.Email).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := test.NewUserQuerySet(db).
		EmailEq(u.Email).
		Delete()
	assert.Nil(t, err)
}

func testUserDeleteByPK(t *testing.T, m sqlmock.Sqlmock, db *gorm.DB) {
	u := getUser()
	req := "UPDATE `users` SET deleted_at=? WHERE `users`.deleted_at IS NULL AND `users`.`id` = ?"
	m.ExpectExec(fixedFullRe(req)).
		WithArgs(sqlmock.AnyArg(), u.ID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	assert.Nil(t, u.Delete(db))
}

func TestMain(m *testing.M) {
	err := GenerateQuerySets("test/models.go", "test/autogenerated_models.go")
	if err != nil {
		panic(err)
	}

	os.Exit(m.Run())
}

func BenchmarkHello(b *testing.B) {
	for i := 0; i < b.N; i++ {
		err := GenerateQuerySets("test/models.go", "test/autogenerated_models.go")
		if err != nil {
			b.Fatalf("can't generate querysets: %s", err)
		}
	}
}
