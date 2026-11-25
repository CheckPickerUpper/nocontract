-module(nocontract_request_recovery_ffi).
-export([rescue/1]).

rescue(Dispatch) ->
    try {ok, Dispatch()}
    catch
        error:Reason:Stack -> crash({errored, Reason}, error, Reason, Stack);
        throw:Reason:Stack -> crash({thrown, Reason}, throw, Reason, Stack);
        exit:Reason:Stack -> crash({exited, Reason}, exit, Reason, Stack)
    end.

crash(Cause, Class, Reason, Stack) ->
    Written = unicode:characters_to_binary(erl_error:format_exception(Class, Reason, Stack)),
    {error, {native_crash, Cause, Written}}.
