# dumpeventstore

Dump the contents of an event store via its atom feed. This works agains the atom feed from the xtracdev oraeventstore and pgeventstore projects.

To use, set the environment variables as per the setenv-template, then run the program.

The dump is written to standard out, and the program activity logging is written to standard out, which means you can redirect the output to stdout and still see what's happening on the console.
