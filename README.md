# dolphin-slippi-tools

Currently pretty simple tool, maybe not the best way to do things but it was a lot easier writing this stuff in Go than C++ to save time for me.

Supports two operations:

`dolphin-slippi-tools user-update`

Will update the User.json of the logged in player. This keeps their connect code in sync with the database as well as updates the latestVersion used to tell the game whether there is an update available.

`dolphin-slippi-tools app-update`

Closes dolphin and updates it by unzipping and overwritting specific files. Not really the most elegant update solution but it did the job for release...
