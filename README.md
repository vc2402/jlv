# jlv
## The very simple json log viewer
jlv is the Go program for viewing log files in json format with vi-like commands.
At the moment it supports listing, filtering and searching on Linux systems.

### Installing

Requires go 1.11+

```
go get github.com/vc2402/jlv
go build github.com/vc2402/jlv
```
### Using

Make sure that bin in your PATH (whether add ~/go/bin to your PATH or put jlv from there in directory that ii in the PATH)

```
jlv <file-name>
```

##### For moving use cursor keys (***up down pgup pgdown home end***)

##### For filtering: 

`:f/<tag>/<value>/<opts>`, where:
- tag - tag to filter (use tab to select from known tags)
- value - value to compare tag's value with
- opts - optional options ('+' - equal to or greater than; '-' - equal to or less than; '$' - regexp)

##### Searching in tag:

`:s/<tag>/<value>/[$]`
looks for value in tag's value as substring; '$' means that value is regexp

##### Searching in whole line (before unmarshalling, so including tags names and json formatting symbols):
```
/<value>
?<value>
```

##### To view full record press ***Enter***

### Plans

- [ ] add check and reread if file modified (new lines added)
- [ ] add posibility of reverse file
- [ ] add streaming functionality
- [ ] add multithreading (background filtering and searching)
- [ ] add web browsing functionality (api + simple gui)
