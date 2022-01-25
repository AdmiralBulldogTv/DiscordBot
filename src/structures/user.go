package structures

import "go.mongodb.org/mongo-driver/bson/primitive"

type User struct {
	ID primitive.ObjectID `bson:"_id,omitempty"`

	Discord UserDiscord `bson:"discord,omitempty"`
	Stream  UserSteam   `bson:"stream"`
	Twitch  UserTwitch  `bson:"twitch"`

	Modules UserModules `bson:"modules"`
}

type UserDiscord struct {
	ID            string `bson:"id"`
	Name          string `bson:"name"`
	Discriminator string `bson:"discriminator"`
}

type UserSteam struct {
	ID   string `bson:"id"`
	Name string `bson:"name"`
}

type UserTwitch struct {
	ID   string `bson:"id"`
	Name string `bson:"name"`
}

type UserModules struct {
	Points UserModulesPoints `bson:"points"`
}

type UserModulesPoints struct {
	Points int32 `bson:"points"`
}
