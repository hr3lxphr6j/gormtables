package models

// User is an application user. It is included via BaseModel embedding.
type User struct {
	BaseModel
	Name string
}

// Post belongs to User (the foreign key UserID lives in this struct).
// AutoMigrate must create the users table before posts.
type Post struct {
	BaseModel
	UserID uint64
	User   *User `gorm:"foreignKey:UserID"`
	Title  string
}

// Tag is included explicitly via the enable marker.
// AutoMigrate:enable
type Tag struct {
	Label string
}

// Draft is explicitly disabled even though it embeds BaseModel.
// AutoMigrate:disable
type Draft struct {
	BaseModel
	Body string
}

// helper is a plain struct with no base embed and no marker; it must be excluded.
type helper struct{}
