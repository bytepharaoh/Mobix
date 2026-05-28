// File for configuration on migrations of mongodb

// Load .env file
require("dotenv").config();

// Getting vars from .env file
const {
  MONGO_USER,
  MONGO_PASSWORD,
  MONGO_HOST,
  MONGO_PORT,
  MONGO_DB,
} = process.env;

// Specifying info about our mongodb
module.exports = {
  mongodb: {
    url: `mongodb://${MONGO_USER}:${MONGO_PASSWORD}@${MONGO_HOST}:${MONGO_PORT}`,
    databaseName: MONGO_DB,

    options: {}
  },

  // Directory in which store migrations
  migrationsDir: "db/migrations",

  // We create a collection inside mongodb with this name
  // which stores information about current migration version
  changelogCollectionName: "changelog",

  migrationFileExtension: ".js",

  // This defines a JavaScript module system
  // (one of 2 ways in Nodejs to organize code sharing (imports) between files)
  moduleSystem: "commonjs"
};