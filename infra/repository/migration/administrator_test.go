package migration

import (
	"context"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/good-fish-man/agent-runtime-client/infra/data"
	userpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/user"
)

func TestSeedAdministratorRequiresExplicitBootstrapPassword(t *testing.T) {
	db := openAdministratorTestDB(t)
	if err := seedAdministrator(context.Background(), data.New(db), BootstrapAdmin{Username: "athena"}); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&userpo.SysUser{}).Where("member_code = ?", "athena").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("administrator was created without an explicit bootstrap password: count=%d", count)
	}
}

func TestSeedAdministratorHashesInjectedPassword(t *testing.T) {
	db := openAdministratorTestDB(t)
	const password = "installation-generated-test-password"
	if err := seedAdministrator(context.Background(), data.New(db), BootstrapAdmin{Username: "athena", Password: password}); err != nil {
		t.Fatal(err)
	}
	var user userpo.SysUser
	if err := db.Where("member_code = ?", "athena").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	if user.AdminLevel != adminAccessLevel || user.RoleId != defaultAdminRoleID || user.State != activeUserState {
		t.Fatalf("bootstrap administrator has unexpected privileges: %#v", user)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		t.Fatalf("injected password was not stored as a bcrypt hash: %v", err)
	}
}

func TestSeedAdministratorNeverElevatesExistingUsername(t *testing.T) {
	db := openAdministratorTestDB(t)
	existing := userpo.SysUser{MemberCode: "athena", NickName: "Existing user", Password: "existing-hash", State: 2, AdminLevel: 0, RoleId: "member"}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}
	if err := seedAdministrator(context.Background(), data.New(db), BootstrapAdmin{Username: "athena", Password: "new-password"}); err != nil {
		t.Fatal(err)
	}
	var user userpo.SysUser
	if err := db.Where("member_code = ?", "athena").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	if user.Password != existing.Password || user.AdminLevel != 0 || user.RoleId != existing.RoleId || user.State != existing.State {
		t.Fatalf("existing username was modified or elevated: %#v", user)
	}
}

func openAdministratorTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&userpo.SysUser{}); err != nil {
		t.Fatal(err)
	}
	return db
}
