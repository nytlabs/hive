![hive-logo-sm](https://cloud.githubusercontent.com/assets/3397/5253268/57e86618-7973-11e4-9199-170cec4f4fe3.png)

# hive

A platform for backing crowdsourcing websites, built in [Go](http://golang.org/) for [Elasticsearch](http://elasticsearch.org).

## Setup

Hive requires elasticsearch version 1.3 or higher. Where you install it is up to you, as you can tell `hive` the domain and port for accessing elasticsearch at startup.

Installation on a Mac is simple with [homebrew](http://brew.sh/):

```
brew update
brew install elasticsearch
```

You can find instructions for other platforms in the [elasticsearch guide](http://www.elasticsearch.org/guide/en/elasticsearch/reference/current/setup.html).

There are two options for running hive.

### Binary

Download the [latest release](https://github.com/nytlabs/hive/releases) and double click to start. `hive` will be running on http://localhost:8080 by default.

### Source

This method will allow you to hack on the `hive` source code. You'll need `go` installed and a working environment for it ($GOPATH, etc).

```
mkdir -p $GOPATH/src/github.com/nytlabs
cd $GOPATH/src/github.com/nytlabs
git clone git@github.com:nytlabs/hive.git
cd hive
make
```

Finally, to start up hive with defaults:

```
./build/hive-server
2014/10/16 14:52:19 running hive-server on port 8080 storing data in elasticsearch under index hive
```

An example specifying all config params:

```
$ ./build/hive-server -index hive -esDomain localhost -esPort=9200 -port 8888
2014/10/16 14:51:54 running hive-server on port 8888 storing data in a local instance of elasticsearch under index hive
```

Forget what parameters are available? There's help:

```
$ ./build/hive-server -h

Usage of ./build/hive-server:
  -esDomain="localhost": elasticsearch domain
  -esPort="9200": elasticsearch port
  -index="hive": elasticsearch index name
  -port="8080": hive port
```

## Importing Data

All of a project's information is defined in JSON and POST'd to `hive` at its admin setup endpoint. You can find [a full example in this repo](https://github.com/nytlabs/hive/blob/master/samples/example.json). 

```
$ curl -XPOST localhost:8080/admin/setup -d@samples/example.json
2014/11/21 12:29:22 Created project: crowd
2014/11/21 12:29:22 task: 2
2014/11/21 12:29:22 assets: 4
```

### Projects

A project is a single crowdsourcing app hosted in hive. Everything is scoped to a project, at the very least: assets, assignments, tasks and users.

Field  | Description
------------- | -------------
Id  | a unique identifier used as a slug in urls
Name  | a regular string title for the project
Description | optional, additional information about the project


```json
  "Project": {
    "Id": "crowd",
    "Name": "Crowd",
    "Description": "An example crowd sourcing site."
  }
```

### Tasks

Tasks are individual actions to do on an asset. A project can have one or more tasks. Criteria for assignment and verification of assets is stored on a task.


Field  | Description
------------- | -------------
Name  | a regular string title for the task
Description | optional additional information
CurrentState | should the task be in the 'available' or 'waiting' state after importing
AssignmentCriteria | the criteria used to assign assets for this task
CompletionCriteria | the criteria used to mark an asset as 'completed' for this task: Total and Matching counts for submissions


```json
  "Tasks": [
    {
      "Name": "categorize",
      "Description": "categorize images",
      "CurrentState": "available",
      "AssignmentCriteria": {
        "SubmittedData": {
          "categorize": {}
        }
      },
      "CompletionCriteria": {
        "Total": 50,
        "Matching": 50
      }
    }
   ]
```

### Assets

Assets are what get assigned to users and can be images, pdfs, etc. All require a URL and are scoped to a project.


Field  | Description
------------- | -------------
Url | required, where to find this asset
Name  | optional, a regular string title
Metadata | optional, any additional data about this asset, specified as key-value pairs.


```json
  "Assets": [
    {
      "Name": "Space 1",
      "Url": "http://upload.wikimedia.org/wikipedia/commons/8/84/Wormhole.png"
    }
   ]
```

## Users

Users are the members of the crowd that you source in your app. They are scoped to a project, so the same person can have multiple records, one per project. Which fields are required is up to you - Hive will create a user with only an ID, to keep the barrier of entry low.

The current user is determined by a cookie named `{project_id}_user_id`, for example, `crowd_user_id`. This cookie should contain the id for the current user.

### Create

**POST** /projects/{project_id}/user

**Response**

```json
{
    "Id": "GorJ0TxVRbipE9SIJypEVQ",
    "Name": "Resourceful Person",
    "Email": "person@example.com",
    "Project": "crowd",
    "ExternalId": "",
    "Counts": {
        "Assignments": 10,
        "Favorites": 0,
        "crowd-categorize": 10,
        "crowd-vote": 0
    },
    "Favorites": {}
}
```

Your site should set the user_id cookie with the Id value returned in this response.

### Get the current user

**GET** /projects/{project_id}/user

**Cookie** {project_id}_user_id

**Response**

```json
{
    "Id": "GorJ0TxVRbipE9SIJypEVQ",
    "Name": "Resourceful Person",
    "Email": "person@example.com",
    "Project": "crowd",
    "ExternalId": "",
    "Counts": {
        "Assignments": 10,
        "Favorites": 0,
        "crowd-categorize": 10,
        "crowd-vote": 0
    },
    "Favorites": {}
}
```

## Assignments

Assignments are the work users have to do for a given task and asset. A user cannot get the same assignment twice: assignments are scoped to the current project, task, asset and user. 

### Create an Assignment

**GET** /projects/{project_id}/tasks/{task_id}/assignments

**Cookie** {project_id}_user_id

**Response**

```json
{
    "Id": "crowdHIVEcrowd-voteHIVExpZWabTwQFS94YgZdK-O-gHIVEGorJ0TxVRbipE9SIJypEVQ",
    "User": "GorJ0TxVRbipE9SIJypEVQ",
    "Project": "crowd",
    "Task": "crowd-vote",
    "Asset": {
        "Id": "xpZWabTwQFS94YgZdK-O-g",
        "Project": "crowd",
        "Url": "",
        "Name": "Space 3",
        "Metadata": {
        },
        "SubmittedData": {
            "categorize": null,
            "vote": null
        },
        "Verified": false,
        "Counts": {
            "Assignments": 2,
            "Favorites": 0,
            "finished": 1,
            "skipped": 0,
            "unfinished": 1
        }
    },
    "State": "unfinished",
    "SubmittedData": null
}
```

Calling this endpoint will find or create an unfinished task assignment for the current user. 

### Submit or Skip an Assignment

**POST** /projects/{project_id}/tasks/{task_id}/assignments

**Cookie** {project_id}_user_id

**Response**


```json
{
    "Id": "crowdHIVEcrowd-voteHIVExpZWabTwQFS94YgZdK-O-gHIVEGorJ0TxVRbipE9SIJypEVQ",
    "User": "GorJ0TxVRbipE9SIJypEVQ",
    "Project": "crowd",
    "Task": "crowd-vote",
    "Asset": {
        "Id": "xpZWabTwQFS94YgZdK-O-g",
        "Project": "crowd",
        "Url": "http://blogs.scientificamerican.com/observations/files/2013/08/Black_Hole_Milkyway.jpg",
        "Name": "Space 3",
        "Metadata": {
        },
        "SubmittedData": {
            "categorize": null,
            "vote": null
        },
        "Verified": false,
        "Counts": {
            "Assignments": 2,
            "Favorites": 0,
            "finished": 1,
            "skipped": 0,
            "unfinished": 1
        }
    },
    "State": "finished",
    "SubmittedData": {
    	"Category": "usable"
    }
}
```

Simply post back an updated version of the JSON in the Create Assignment response to submit it (State: finished) or skip it (State: skipped). 

### Create an Assignment for a Specific Asset

**GET** /projects/{project_id}/tasks/{task_id}/assets/{asset_id}/assignments

**Cookie** {project_id}_user_id

**Response** Same as the more general 'create assignment' response

Use this endpoint in situations where you're displaying assets on your site and want to allow users to act on those specifically, rather than a random available valid asset. Specify an asset id and get back an assignment on it for the current user, project and task.

### Lookup an Assignment by Id

**GET** /projects/{project_id}/assignments/{assignment_id}

**Response**

```json
{
    "Id": "crowdHIVEcrowd-voteHIVExpZWabTwQFS94YgZdK-O-gHIVEGorJ0TxVRbipE9SIJypEVQ",
    "User": "GorJ0TxVRbipE9SIJypEVQ",
    "Project": "crowd",
    "Task": "crowd-vote",
    "Asset": {
        "Id": "xpZWabTwQFS94YgZdK-O-g",
        "Project": "crowd",
        "Url": "",
        "Name": "Space 3",
        "Metadata": {
        },
        "SubmittedData": {
            "categorize": null,
            "vote": null
        },
        "Verified": false,
        "Counts": {
            "Assignments": 2,
            "Favorites": 0,
            "finished": 1,
            "skipped": 0,
            "unfinished": 1
        }
    },
    "State": "unfinished",
    "SubmittedData": null
}
```

Returns information for a single assignment by id.

## Assets

Actions available for assets outside of the admin.

### Get an Asset

**GET** /projects/{project_id}/assets/{asset_id}

**Response**

```json
{
    "Asset": {
        "Id": "AUnTaQpqzTmtUIq-fdvJ",
        "Project": "crowd",
        "Url": "http://blogs.scientificamerican.com/observations/files/2013/08/Black_Hole_Milkyway.jpg",
        "Name": "Space 3",
        "Metadata": null,
        "SubmittedData": {
            "categorize": null,
            "vote": null
        },
        "Verified": false,
        "Counts": {
            "Assignments": 0,
            "finished": 0,
            "skipped": 0,
            "unfinished": 0
        }
    }
}
```

This endpoint returns information for a single asset.

### Favorite/Unfavorite an Asset

**GET** /projects/{project_id}/assets/{asset_id}/favorite

**Cookie** {project_id}_user_id

**Response**

Favoriting: 

```json
{
    "AssetId": "AUnTaQpqzTmtUIq-fdvJ",
    "Action": "favorited"
}
```

Unfavoriting:

```json
{
    "AssetId": "AUnTaQpqzTmtUIq-fdvJ",
    "Action": "unfavorited"
}
```

This endpoint toggles favoriting or unfavoriting an asset for the current user.

## Tasks

Actions available for tasks outside of the admin.

### Get Tasks

**GET** /projects/{project_id}/tasks

**Response**

```json
{
    "Tasks": [
        {
            "Id": "crowd-categorize",
            "Project": "crowd",
            "Name": "categorize",
            "Description": "categorize images",
            "CurrentState": "available",
            "AssignmentCriteria": {
                "SubmittedData": {
                    "categorize": {}
                }
            },
            "CompletionCriteria": {
                "Total": 50,
                "Matching": 50
            }
        },
        {
            "Id": "crowd-vote",
            "Project": "crowd",
            "Name": "vote",
            "Description": "vote on image quality (from 1 to 10)",
            "CurrentState": "waiting",
            "AssignmentCriteria": {
                "SubmittedData": {
                    "categorize": {
                        "ad-content": "usable"
                    },
                    "vote": {}
                }
            },
            "CompletionCriteria": {
                "Total": 2,
                "Matching": 2
            }
        }
    ],
    "Meta": {
        "Total": 0,
        "From": 0,
        "Size": 0
    }
}
```

Returns a list of tasks in this project.

### Get Task

**GET** /projects/{project_id}/tasks/{task_id}

**Response**

```json
{
    "Task": {
        "Id": "crowd-categorize",
        "Project": "crowd",
        "Name": "categorize",
        "Description": "categorize images",
        "CurrentState": "available",
        "AssignmentCriteria": {
            "SubmittedData": {
                "categorize": {}
            }
        },
        "CompletionCriteria": {
            "Total": 50,
            "Matching": 50
        }
    }
}
```

Returns information for a single task in this project.



## API Endpoints

Finally, a list of all the API actions.


* **ANY** / - useful for health checks / heartbeats 
* **ANY** /admin/setup - clears out db, configures elasticsearch and creates a project
* **GET** /admin/projects - returns all projects in Hive
* **GET** /admin/projects/{project_id} - returns project information
* **POST** /admin/projects/{project_id} - creates or updates a project
* **GET** /admin/projects/{project_id}/tasks - returns tasks in this project
* **POST** /admin/projects/{project_id}/tasks - imports tasks into this project
* **GET** /admin/projects/{project_id}/tasks/{task_id} - returns task information
* **POST** /admin/projects/{project_id}/tasks/{task_id} - create or update a task
* **enable and disable tasks
* **GET** /admin/projects/{project_id}/assets - returns assets in this project
* **GET** /admin/projects/{project_id}/assets?from=10&size=30 - paginates assets
* **GET** /admin/projects/{project_id}/assets?task=:task&state=:state - returns a list of assets based on task and state
* **POST** /admin/projects/{project_id}/assets - imports assets into this project
* **GET** /admin/projects/{project_id}/assets/{asset_id} - get a single asset's data
* **GET** /admin/projects/{project_id}/tasks/{task_id}/complete - mark any assets completed for this task
* **GET** /admin/projects/{project_id}/users - returns users in this project
* **GET** /admin/projects/{project_id}/users?from=0&size=10 - paginates users
* **GET** /admin/projects/{project_id}/users/{user_id} - returns a single user in this project
* **GET** /admin/projects/{project_id}/assignments?task={task_id}&state={state}
* **GET** /admin/projects/{project_id}/assignments?task={task_id}&state={state}&from=from&size=size
* **GET** /projects/{project_id}/tasks/{task_id} - returns task information
* **GET** /projects/{project_id}/tasks/{task_id}/assignments - returns a new assignment for the given task + current user
* **POST** /projects/{project_id}/tasks/{task_id}/assignments - submit assignment (contribute, fill in form, etc)
* **GET** /projects/{project_id} - returns project information
* **GET** /projects/{project_id}/assets/{asset_id} - returns asset information
* **GET** /projects/{project_id}/tasks - returns tasks in this project
* **GET** /projects/{project_id}/tasks/{task_id}/assets/{asset_id}/assignments - returns a new assignment for task + asset + current user
* **GET** /projects/{project_id}/user - returns user information based on project session cookie
* **POST** /projects/{project_id}/user - creates a user based on json data posted
* **POST** /projects/{project_id}/user/external - looks up user by external id, returns session token
* **GET** /projects/{project_id}/assets/{asset_id}/favorite - favorites an asset
* **GET** /projects/{project_id}/user/favorites - returns a user's favorited ads
* **GET** /projects/{project_id}/assignments/{assignment} - returns assignment information
