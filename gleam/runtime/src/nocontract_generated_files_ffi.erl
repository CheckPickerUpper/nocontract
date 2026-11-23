-module(nocontract_generated_files_ffi).
-export([check/1]).

check(Arguments) ->
    Started = erlang:monotonic_time(millisecond),
    case file:get_cwd() of
        {ok, Directory} ->
            find_go(Directory, Arguments, Started);
        {error, Cause} ->
            {native_directory_unreadable, Cause, elapsed(Started)}
    end.

find_go(Directory, Arguments, Started) ->
    case os:find_executable("go") of
        false ->
            {native_go_not_found, unicode:characters_to_binary(Directory), elapsed(Started)};
        Executable ->
            launch(Directory, Executable, Arguments, Started)
    end.

launch(Directory, Executable, Arguments, Started) ->
    Caller = self(),
    {Owner, Monitor} = spawn_monitor(fun() -> own_port(Caller, Directory, Executable, Arguments) end),
    case await_owner(Owner, Monitor, []) of
        {not_started, Cause} ->
            {native_not_started, unicode:characters_to_binary(Directory),
                unicode:characters_to_binary(Executable), Cause, elapsed(Started)};
        {completed, Process} ->
            {native_completed, unicode:characters_to_binary(Directory),
                unicode:characters_to_binary(Executable), Process, elapsed(Started)}
    end.

own_port(Caller, Directory, Executable, Arguments) ->
    process_flag(trap_exit, true),
    CallerMonitor = erlang:monitor(process, Caller),
    case open_check(Directory, Executable, Arguments) of
        {ok, Port} ->
            Collected = close_check(Port, collect(Port, Caller, CallerMonitor, pending)),
            Caller ! {self(), {completed, Collected}};
        {error, Cause} ->
            Caller ! {self(), {not_started, Cause}}
    end.

await_owner(Owner, Monitor, Chunks) ->
    receive
        {Owner, {output, Bytes}} ->
            await_owner(Owner, Monitor, [Bytes | Chunks]);
        {Owner, {not_started, Cause}} ->
            erlang:demonitor(Monitor, [flush]),
            {not_started, Cause};
        {Owner, {completed, Process}} ->
            erlang:demonitor(Monitor, [flush]),
            {completed, captured_process(Process, Chunks)};
        {'DOWN', Monitor, process, Owner, Cause} ->
            {completed, captured_process({interrupted, Cause}, Chunks)}
    end.

captured_process({exited, Status}, Chunks) ->
    {native_exited, Status, iolist_to_binary(lists:reverse(Chunks))};
captured_process({interrupted, Cause}, Chunks) ->
    {native_interrupted, Cause, iolist_to_binary(lists:reverse(Chunks))}.

open_check(Directory, Executable, Arguments) ->
    try erlang:open_port({spawn_executable, Executable}, [
        binary, exit_status, eof, use_stdio, stderr_to_stdout,
        {cd, Directory}, {args, Arguments}
    ]) of
        Port -> {ok, Port}
    catch
        error:Cause -> {error, Cause}
    end.

close_check(Port, {port_open, Collected}) ->
    erlang:port_close(Port),
    Collected;
close_check(_Port, {port_closed, Collected}) -> Collected.

collect(Port, Caller, CallerMonitor, Status) ->
    receive
        {Port, {data, Bytes}} ->
            Caller ! {self(), {output, Bytes}},
            collect(Port, Caller, CallerMonitor, Status);
        {Port, {exit_status, ExitStatus}} ->
            collect(Port, Caller, CallerMonitor, {exited, ExitStatus});
        {Port, eof} ->
            wait_for_exit(Port, Caller, CallerMonitor, Status);
        {'EXIT', Port, Cause} ->
            {port_closed, {interrupted, Cause}};
        {'DOWN', CallerMonitor, process, Caller, Cause} ->
            {port_open, {interrupted, Cause}}
    end.

wait_for_exit(Port, Caller, CallerMonitor, pending) ->
    receive
        {Port, {exit_status, Status}} ->
            {port_open, {exited, Status}};
        {'EXIT', Port, Cause} ->
            {port_closed, {interrupted, Cause}};
        {'DOWN', CallerMonitor, process, Caller, Cause} ->
            {port_open, {interrupted, Cause}}
    end;
wait_for_exit(_Port, _Caller, _CallerMonitor, {exited, Status}) ->
    {port_open, {exited, Status}}.

elapsed(Started) -> erlang:monotonic_time(millisecond) - Started.
