package postgresql

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func testAccCheckPostgresqlSubscriptionDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "postgresql_subscription" {
			continue
		}

		databaseName, ok := rs.Primary.Attributes["database"]
		if !ok {
			return fmt.Errorf("No Attribute for database is set")
		}
		txn, err := startTransaction(client, databaseName)
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		subName := rs.Primary.Attributes["name"]
		exists, err := checkSubscriptionExists(txn, subName)

		if err != nil {
			return fmt.Errorf("error checking subscription %s", err)
		}

		if exists {
			return fmt.Errorf("Subscription still exists after destroy")
		}

		streams, err := checkSubscriptionStreams(txn, subName)

		if err != nil {
			return fmt.Errorf("error checking subscription %s", err)
		}

		if streams {
			return fmt.Errorf("Subscription still streams after destroy")
		}
	}

	return nil
}

func checkSubscriptionExists(txn *sql.Tx, subName string) (bool, error) {
	var _rez bool
	err := txn.QueryRow("SELECT TRUE from pg_catalog.pg_subscription WHERE subname=$1", subName).Scan(&_rez)

	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("error reading info about subscription: %s", err)
	}

	return true, nil
}

func checkSubscriptionStreams(txn *sql.Tx, subName string) (bool, error) {
	var _rez bool
	err := txn.QueryRow("SELECT TRUE from pg_catalog.pg_stat_replication WHERE application_name=$1 and state='streaming'", subName).Scan(&_rez)

	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("error reading info about subscription: %s", err)
	}

	return true, nil
}

func testAccCheckPostgresqlSubscriptionExists(n string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Resource not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No ID is set")
		}

		databaseName, ok := rs.Primary.Attributes["database"]
		if !ok {
			return fmt.Errorf("No Attribute for database is set")
		}

		subName, ok := rs.Primary.Attributes["name"]
		if !ok {
			return fmt.Errorf("No Attribute for subscription name is set")
		}

		client := testAccProvider.Meta().(*Client)
		txn, err := startTransaction(client, databaseName)

		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		exists, err := checkSubscriptionExists(txn, subName)

		if err != nil {
			return fmt.Errorf("error checking subscription %s", err)
		}

		if !exists {
			return fmt.Errorf("Subscription not found")
		}

		streams, err := checkSubscriptionStreams(txn, subName)
		if err != nil {
			return fmt.Errorf("error checking subscription %s", err)
		}
		if !streams {
			return fmt.Errorf("Subscription not streaming")
		}

		return nil
	}
}

func getConnInfo(t *testing.T, dbName string) string {
	dbConfig := getTestConfig(t)

	return fmt.Sprintf(
		`host=%s port=%d dbname=%s user=%s password=%s`,
		dbConfig.Host,
		5432,
		dbName,
		dbConfig.Username,
		dbConfig.Password,
	)
}

// The database seems to take a few second to cleanup everything
func coolDown() {
	time.Sleep(5 * time.Second)
}

func TestAccPostgresqlSubscription_Basic(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffixPub, teardownPub := setupTestDatabase(t, true, true)
	dbSuffixSub, teardownSub := setupTestDatabase(t, true, true)

	defer teardownPub()
	defer teardownSub()
	testTables := []string{"test_schema.test_table_1"}
	createTestTables(t, dbSuffixPub, testTables, "")
	createTestTables(t, dbSuffixSub, testTables, "")

	dbNamePub, _ := getTestDBNames(dbSuffixPub)
	dbNameSub, _ := getTestDBNames(dbSuffixSub)

	conninfo := getConnInfo(t, dbNamePub)

	subName := "subscription"
	testAccPostgresqlSubscriptionDatabaseConfig := fmt.Sprintf(`
	resource "postgresql_publication" "test_pub" {
		name     	= "test_publication"
		database	= "%s"
		tables		= ["test_schema.test_table_1"]
	}
	resource "postgresql_replication_slot" "test_replication_slot" {
		name		= "%s"
		database	= "%s"
		plugin		= "pgoutput"
	}
	resource "postgresql_subscription" "test_sub" {
		name     		= postgresql_replication_slot.test_replication_slot.name
		database 		= "%s"
		conninfo 		= "%s"
		publications	= [ postgresql_publication.test_pub.name ]
		create_slot		= false
	}
	`, dbNamePub, subName, dbNamePub, dbNameSub, conninfo)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlSubscriptionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlSubscriptionDatabaseConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlSubscriptionExists(
						"postgresql_subscription.test_sub"),
					resource.TestCheckResourceAttr(
						"postgresql_subscription.test_sub",
						"name",
						subName),
					resource.TestCheckResourceAttr(
						"postgresql_subscription.test_sub",
						"database",
						dbNameSub),
					resource.TestCheckResourceAttr(
						"postgresql_subscription.test_sub",
						"conninfo",
						conninfo),
					resource.TestCheckResourceAttr(
						"postgresql_subscription.test_sub",
						fmt.Sprintf("%s.#", "publications"),
						"1"),
					resource.TestCheckResourceAttr(
						"postgresql_subscription.test_sub",
						fmt.Sprintf("%s.0", "publications"),
						"test_publication"),
					resource.TestCheckResourceAttr(
						"postgresql_subscription.test_sub",
						"create_slot",
						"false"),
				),
			},
		},
	},
	)
	coolDown()
}

func TestAccPostgresqlSubscription_CustomSlotName(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffixPub, teardownPub := setupTestDatabase(t, true, true)
	dbSuffixSub, teardownSub := setupTestDatabase(t, true, true)

	defer teardownPub()
	defer teardownSub()

	dbNamePub, _ := getTestDBNames(dbSuffixPub)
	dbNameSub, _ := getTestDBNames(dbSuffixSub)

	conninfo := getConnInfo(t, dbNamePub)

	subName := "subscription"
	testAccPostgresqlSubscriptionDatabaseConfig := fmt.Sprintf(`
	resource "postgresql_publication" "test_pub" {
		name		= "test_publication"
		database	= "%s"
	}
	resource "postgresql_replication_slot" "test_replication_slot" {
		name		= "custom_slot_name"
		plugin		= "pgoutput"
		database	= "%s"
	}
	resource "postgresql_subscription" "test_sub" {
		name     		= "%s"
		database 		= "%s"
		conninfo 		= "%s"
		publications	= [ postgresql_publication.test_pub.name ]
		create_slot		= false
		slot_name		= "custom_slot_name"

		depends_on 		= [ postgresql_replication_slot.test_replication_slot ]
	}
	`, dbNamePub, dbNamePub, subName, dbNameSub, conninfo)
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlSubscriptionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlSubscriptionDatabaseConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlSubscriptionExists(
						"postgresql_subscription.test_sub"),
					resource.TestCheckResourceAttr(
						"postgresql_subscription.test_sub",
						"name",
						subName),
					resource.TestCheckResourceAttr(
						"postgresql_subscription.test_sub",
						"database",
						dbNameSub),
					resource.TestCheckResourceAttr(
						"postgresql_subscription.test_sub",
						"conninfo",
						conninfo),
					resource.TestCheckResourceAttr(
						"postgresql_subscription.test_sub",
						fmt.Sprintf("%s.#", "publications"),
						"1"),
					resource.TestCheckResourceAttr(
						"postgresql_subscription.test_sub",
						fmt.Sprintf("%s.0", "publications"),
						"test_publication"),
					resource.TestCheckResourceAttr(
						"postgresql_subscription.test_sub",
						"create_slot",
						"false"),
					resource.TestCheckResourceAttr(
						"postgresql_subscription.test_sub",
						"slot_name",
						"custom_slot_name"),
				),
			},
		},
	},
	)
	coolDown()
}

// Read must clear the ID of a subscription that no longer exists. The Exists
// callback hides this today; Read is called directly to pin it down.
func TestAccPostgresqlSubscription_ReadMissing(t *testing.T) {
	skipIfNotAcc(t)
	testAccPreCheck(t)
	testCheckCompatibleVersion(t, featurePublication)

	client := testAccProvider.Meta().(*Client)
	db, err := client.Connect()
	if err != nil {
		t.Fatalf("could not connect: %v", err)
	}

	const subName = "tf_tests_missing_sub"
	d := schema.TestResourceDataRaw(t, resourcePostgreSQLSubscription().Schema, map[string]any{
		"name":         subName,
		"database":     client.databaseName,
		"conninfo":     "host=localhost",
		"publications": []any{"tf_tests_pub"},
	})
	d.SetId(client.databaseName + "." + subName)

	if err := resourcePostgreSQLSubscriptionReadImpl(db, d); err != nil {
		t.Fatalf("reading a missing subscription returned an error: %v", err)
	}
	if d.Id() != "" {
		t.Fatalf("expected the ID to be cleared, got %q", d.Id())
	}
}
