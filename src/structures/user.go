package structures

import "go.mongodb.org/mongo-driver/bson/primitive"

type User struct {
	ID primitive.ObjectID `bson:"_id,omitempty"`

	Discord UserDiscord `bson:"discord,omitempty"`
	Steam   UserSteam   `bson:"steam,omitempty"`
	Twitch  UserTwitch  `bson:"twitch,omitempty"`

	Modules UserModules `bson:"modules"`
}

type UserDiscord struct {
	ID            string `bson:"id,omitempty"`
	Name          string `bson:"name,omitempty"`
	Discriminator string `bson:"discriminator,omitempty"`
}

type UserSteam struct {
	ID   string `bson:"id,omitempty"`
	Name string `bson:"name,omitempty"`
}

type UserTwitch struct {
	ID   string `bson:"id,omitempty"`
	Name string `bson:"name,omitempty"`
}

type UserModules struct {
	Points UserModulesPoints `bson:"points"`
}

type UserModulesPoints struct {
	Points int32 `bson:"points"`
}
