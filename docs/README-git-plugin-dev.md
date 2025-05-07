
# Notes about the development of git plugins


To attach the plugin into the configutation. In the global config `~/.gitconfig` add the lines:
```
[filter "drs"]
	clean = git-drs clean -- %f
	smudge = git-drs smudge -- %f
	process = git-drs filter-process
	required = true
```

Then to add tracking in a project, add entries to `.gitattributes` in the working directory. Example:
```
*.tsv filter=drs diff=drs merge=drs -text
```

For when `git status` or `git diff` are invoked on `*.tsv` file, the process `git-drs filter-process` will be
invoked. The communication between git and the subprocess is outlined in (gitprotocol-common)[https://git-scm.com/docs/gitprotocol-common]. A library for parsing this event stream is part of the git-lfs code base https://github.com/git-lfs/git-lfs/blob/main/git/filter_process_scanner.go 
An example of responding to these requests can be found at https://github.com/git-lfs/git-lfs/blob/main/commands/command_filter_process.go 

My understanding: The main set of command the the filter-process command responds to are `clean` and `smudge`. 
The `clean` process cleans an input document before running diff, things like run auto formatting before committing. This is where the change from the file to the remote data pointer could take place. An example of the 
clean process can be found at https://github.com/git-lfs/git-lfs/blob/main/commands/command_clean.go#L27