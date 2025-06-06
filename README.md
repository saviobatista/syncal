# Syncal - Google Calendar to iCloud Sync

A headless Go service to synchronize events from multiple Google Calendar accounts to a single Apple iCloud Calendar. It runs as a background service, making it perfect for a home lab deployment in a Docker container.

---

## Features

- **Multi-Account Sync**: Sync events from several Google Calendar accounts into one iCloud calendar.
- **Headless OAuth2**: A CLI-based authentication flow to get Google API tokens without a dedicated web server.
- **CalDAV Integration**: Uses the CalDAV protocol to interact with Apple's iCloud calendars.
- **Duplicate Prevention**: Keeps track of synced events to avoid creating duplicate entries.
- **Flexible Sync Modes**: Run once, run on a schedule (`--watch`), or perform a no-op with `--dry-run`.
- **Dockerized**: Comes with a multi-stage `Dockerfile` for a small, static container image.
- **Structured Logging**: Clear, structured logs for easy debugging.

---

## Setup & Configuration

### 1. Prerequisites

- Go (v1.21 or later)
- Docker & Docker Compose
- Access to a Google Cloud Platform project
- An Apple ID with an [app-specific password](https://support.apple.com/en-us/HT204397)

### 2. Configure Environment Variables

The application is configured using environment variables. Copy the example file to a new `.env` file:

```bash
cp .env.example .env
```

You will need to fill in the values in this `.env` file.

#### **Obtaining Google API Credentials**

1.  **Go to the [Google Cloud Console](https://console.cloud.google.com/).**
2.  Create a new project or select an existing one.
3.  **Enable the Google Calendar API**: In the navigation menu, go to `APIs & Services > Library`, search for "Google Calendar API", and enable it.
4.  **Configure the OAuth Consent Screen**:
    - Go to `APIs & Services > OAuth consent screen`.
    - Choose **External** and click **Create**.
    - Fill in the required fields (app name, user support email, developer contact). You can use dummy information for a personal project.
    - Scopes: You don't need to add scopes here. The application will request them.
    - Test Users: **Add the Google account(s)** you want to sync from as test users while your app is in "Testing" mode. This is important!
5.  **Create OAuth 2.0 Client IDs**:
    - Go to `APIs & Services > Credentials`.
    - Click `+ CREATE CREDENTIALS > OAuth client ID`.
    - Select **Desktop app** as the application type.
    - After creation, a dialog will show your **Client ID** and **Client Secret**. Add these to your `.env` file.
    - You can also download the credentials as `credentials.json`. The app can use this file if environment variables are not set.

#### **Obtaining Apple iCloud Credentials**

1.  **Generate an App-Specific Password**:
    - Sign in to your Apple ID account page: [appleid.apple.com](https://appleid.apple.com/).
    - In the "Sign-In and Security" section, click on **App-Specific Passwords**.
    - Click "Generate an app-specific password" and give it a label (e.g., "Syncal").
    - Copy the generated password and add it to `ICLOUD_APP_SPECIFIC_PASSWORD` in your `.env` file.
2.  **Find your iCloud Calendar Name**:
    - This is the name of the calendar you see in the Calendar app on your Mac or iPhone. The default is often "Calendar" or "Home". Update `ICLOUD_CALENDAR_NAME` accordingly.

### 3. Google Account Authentication (Getting a Token)

Because this is a headless app, you need to perform a one-time authorization step to grant it access to your Google Calendar(s).

Run the `auth` command:

```bash
go run cmd/main.go auth
```

This will:
1.  Print a URL to your console.
2.  Copy and paste this URL into a web browser.
3.  Choose the Google account you want to authorize.
4.  You may see a warning because the app is "unverified." This is expected. Click "Advanced" and proceed.
5.  Grant the "Google Calendar API" permission.
6.  You will be redirected to a localhost URL (which will fail to load, this is normal). Copy the `code` parameter from this URL.
7.  Paste the code back into the terminal when prompted.

The application will exchange this code for an OAuth token and save it as `token.json`. This file will be used for all subsequent API requests. **You must do this for each Google account you want to sync.** The application will guide you to save multiple tokens.

---

## How to Run

### Locally

Make sure you have configured your `.env` file and authenticated with Google.

```bash
# Run a single sync and exit
go run cmd/main.go sync --once

# Run a sync every 5 minutes (default)
go run cmd/main.go sync --watch

# See what would be synced without making any changes
go run cmd/main.go sync --dry-run
```

### With Docker

The provided `Dockerfile` builds the application and can be run easily.

1.  **Build the Docker image**:
    ```bash
    docker build -t syncal .
    ```

2.  **Run the container**:
    Mount your `.env` file and the `token.json` file(s) into the container.

    ```bash
    docker run --rm \
      -v $(pwd)/.env:/app/.env \
      -v $(pwd)/token.json:/app/token.json \
      syncal \
      sync --watch
    ```

    If you have multiple tokens, you'll need to mount each one.

---

## Project Structure

- `cmd/main.go`: CLI entry point, powered by `urfave/cli`.
- `internal/google/`: Google Calendar client and OAuth2 handling.
- `internal/icloud/`: CalDAV client for interacting with iCloud.
- `internal/models/`: Contains the shared `Event` struct.
- `internal/syncer/`: The core logic that orchestrates the sync process.
- `Dockerfile`: Multi-stage Dockerfile for a minimal, secure image.
- `.env.example`: Template for environment variables.
- `sync-state.json`: A simple file to store the state of synced events to prevent duplicates. 