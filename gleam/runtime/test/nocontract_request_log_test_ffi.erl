-module(nocontract_request_log_test_ffi).
-export([capture/1, log/2]).

capture(Emit) ->
    PreviousLevel = maps:get(level, logger:get_primary_config()),
    Handler = nocontract_request_log_test,
    ok = logger:add_handler(Handler, ?MODULE, #{
        level => info,
        config => #{receiver => self()}
    }),
    ok = logger:set_primary_config(level, info),
    try
        Emit(),
        receive
            {request_log_output, Output} -> Output
        after 1000 ->
            erlang:error(request_log_not_emitted)
        end
    after
        ok = logger:remove_handler(Handler),
        ok = logger:set_primary_config(level, PreviousLevel)
    end.

log(Event, #{config := #{receiver := Receiver}}) ->
    Output = logger_formatter:format(Event, #{template => [msg, "\n"], single_line => false}),
    Receiver ! {request_log_output, unicode:characters_to_binary(Output)},
    ok.
