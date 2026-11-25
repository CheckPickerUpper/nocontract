-module(nocontract_shop_request_hold_ffi).
-export([hold/0]).

hold() ->
    case os:getenv("SHOP_HOLD_REQUEST_ADDRESS") of
        false ->
            nil;
        Address ->
            [Host, Port] = string:split(Address, ":"),
            {ok, Socket} = gen_tcp:connect(Host, list_to_integer(Port), [binary, {active, false}], 5000),
            try
                {ok, Release} = gen_tcp:recv(Socket, 1, 5000),
                ok = gen_tcp:send(Socket, Release),
                nil
            after
                gen_tcp:close(Socket)
            end
    end.
