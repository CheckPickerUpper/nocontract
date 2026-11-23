-module(nocontract_request_deadline_ffi).
-export([with_monitored_worker/3, monotonic_milliseconds/0, raise_exit/1]).

monotonic_milliseconds() -> erlang:monotonic_time(millisecond).

raise_exit(normal) -> erlang:exit(normal);
raise_exit(killed) -> erlang:exit(killed);
raise_exit({abnormal, Reason}) -> erlang:exit(Reason).

%% The Subject tag selects the reply; the native monitor alias controls its delivery.
with_monitored_worker(Run, {subject, _Owner, Tag}, Await) ->
    {Pid, Monitor} = proc_lib:spawn_opt(
        fun() ->
            receive
                {nocontract_request_deadline, Alias} -> Alias ! {Tag, Run()}
            end
        end,
        [{monitor, [{alias, explicit_unalias}]}]
    ),
    Pid ! {nocontract_request_deadline, Monitor},
    try Await(Pid, Monitor)
    after
        erlang:unalias(Monitor),
        erlang:demonitor(Monitor, [flush]),
        receive {Tag, _Reply} -> nil after 0 -> nil end,
        erlang:exit_signal(Pid, kill)
    end.
