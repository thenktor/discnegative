# DiscNegative

This is a fork of [Supersonic](https://github.com/supersonic-app/supersonic). I've made the fork to fit the app my personal preferences:

* Removed some features, that I do not need
* Tweaked the UI

[![License](https://img.shields.io/github/license/supersonic-app/supersonic)](https://github.com/thenktor/discnegative/blob/main/LICENSE)
[![Last Release](https://img.shields.io/github/v/release/thenktor/discnegative?logo=github&label=latest&style=flat)](https://github.com/thenktor/discnegative/releases)
[![Downloads](https://img.shields.io/github/downloads/thenktor/discnegative/total?logo=github&style=flat)](https://github.com/thenktor/discnegative/releases/latest)

## Features

* [x] Fast, lightweight, native UI with infinite scrolling
* [x] Light and Dark themes, with optional auto theme switching
* [x] High-quality gapless audio playback powered by MPV, with optional audio exclusive mode
* [x] ReplayGain support (depends on files being tagged on server)
* [x] [Custom themes](https://github.com/supersonic-app/supersonic/wiki/Custom-Themes) 
* [x] Scrobble plays to server, with configurable criteria
* [x] Add and switch between multiple servers
* [x] Primary and alternate server hostnames, e.g. for internal and external URLs
* [x] Set filters in albums browsing view
* [x] Play "artist radio" (mix of songs from given artist and similar artists, depends on your server's support)
* [x] Sort tracklist views by column and configure visible tracklist columns
* [x] Shuffle and repeat playback modes (partial; shuffle album, playlist, artist radio, random songs; repeat one/all)
* [x] Lyrics support
* [x] Internet radio station support (Subsonic)

## Removed features

These features have been removed. If you need them, go upstream to [Supersonic](https://github.com/supersonic-app/supersonic).

* [x] Waveform seekbar
* [x] MPRIS, Windows SMTC, and Mac OS media center integration for media key and desktop control
* [x] Built-in 15-band graphic equalizer
* [x] Download songs, albums or playlists
* [x] Cast to uPnP/DLNA devices

## Supported servers

Supersonic supports any music server with a Subsonic (or OpenSubsonic) API. A partial list of supported servers is as follows:

* [Navidrome](https://navidrome.org) (tested and used by myself)
* [Gonic](https://github.com/sentriz/gonic) (untested)
* [LMS](https://github.com/epoupon/lms) (untested)
* [Nextcloud Music](https://apps.nextcloud.com/apps/music) (untested)
* [Airsonic-Advanced](https://github.com/airsonic-advanced/airsonic-advanced) (untested)
* [Ampache](https://ampache.org) (untested)
* [Funkwhale](https://www.funkwhale.audio/) (untested)
* [Supysonic](https://github.com/spl0k/supysonic) (untested)

# Supersonic

A lightweight cross-platform desktop client for Subsonic-compatible music servers.

[Jump to installation instructions](https://github.com/supersonic-app/supersonic#installation)

## Screenshots

Screenshots of Supersonic running against the Navidrome demo server, showcasing the builtin dark and light themes.

<a href="https://raw.githubusercontent.com/supersonic-app/supersonic/main/res/screenshots/NowPlayingView.png"><img src="https://raw.github.com/supersonic-app/supersonic/main/res/screenshots/NowPlayingView.png" width="49.5%"/></a>
<a href="https://raw.githubusercontent.com/supersonic-app/supersonic/main/res/screenshots/AlbumsView.png"><img src="https://raw.github.com/supersonic-app/supersonic/main/res/screenshots/AlbumsView.png" width="49.5%"/></a>
<a href="https://raw.githubusercontent.com/supersonic-app/supersonic/main/res/screenshots/FavoriteSongsView.png"><img src="https://raw.github.com/supersonic-app/supersonic/main/res/screenshots/FavoriteSongsView.png" width="49.5%"/></a>
<a href="https://raw.githubusercontent.com/supersonic-app/supersonic/main/res/screenshots/ArtistView.png"><img src="https://raw.github.com/supersonic-app/supersonic/main/res/screenshots/ArtistView.png" width="49.5%"/></a>

## Installation

Platform-specific installation instructions are listed below. In addition to the most recent stable release, you can also download the latest build from the `main` branch via the [Actions](https://github.com/supersonic-app/supersonic/actions) tab to get unreleased features and bug fixes (you must be signed in to Github to do this). If you prefer to build from source, then see the [build instructions](BUILD.md) for your platform.

### Linux

* **AppImage:** On the [latest release](https://github.com/supersonic-app/supersonic/releases) page, you can download an AppImage package with the MPV library bundled.<br/>
Tested OSes:<br/> 
ubuntu 22.04+ <br/> 
debian 12+ <br/> 
fedora 42+ <br/> 

* **(Debian) .tar.xz:** The.tar.xz builds from the Releases page support Debian-based distros. You **must** have libmpv installed on your system, and choose the correct release build (libmpv2 or libmpv1) based on which is available in your distro's package manager. On apt-based systems, run `sudo apt install libmpv1` (or libmpv2) if it is not already installed. To install the Linux release build, after ensuring the required libmpv is installed, extract the .tar.xz bundle and run `make user-install` or `sudo make install`.

* **Packages:** On Linux, Supersonic is [available as a Flatpak](https://flathub.org/apps/details/io.github.dweymouth.supersonic). (Thank you @anarcat!) Third-party packages are also available for Arch, Nix OS, and [Fedora (COPR)](https://copr.fedorainfracloud.org/coprs/coffeeicus/supersonic/). **Please note** that the Flatpak package currently does not support CJK fonts as the sandboxing breaks font lookup.

### Windows

Download the [latest release](https://github.com/supersonic-app/supersonic/releases). You can choose between the installer, or a standalone zip file which can be extracted and run without requiring system installation.

### Mac OS

Supersonic is available on Homebrew via a custom brew tap, or via downloading the .app bundle from the Releases page.

**To install Supersonic with Homebrew** run:

```sh
brew tap supersonic-app/supersonic
brew install supersonic
xattr -r -d com.apple.quarantine /Applications/Supersonic.app
```

The `xattr -r -d com.apple.quarantine` command is important because Supersonic is distributed without having been [notarized](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution), and therefore will not run without this.
You should also re-run that xattr command when upgrading in future.

**To install the downloaded .app bundle** from the [Releases page](https://github.com/supersonic-app/supersonic/releases), unzip and then drag Supersonic.app to the Applications folder.

:warning: **Apple Silicon (M1 and newer) Macs:** You will have to remove the "quarantine bit" that Mac will automatically set, being an application downloaded from the internet. After copying the .app bundle to your Applications folder, in the terminal run `xattr -r -d com.apple.quarantine /Applications/Supersonic.app`

## Building from source

Build instructions for Linux, Windows, and Mac are listed [here](BUILD.md)
