defmodule EasyTV.MpvCommandsTest do
  use ExUnit.Case
  import ExUnit.CaptureLog

  setup do
    parent = self()

    start_supervised!(
      {EasyTV.ConfigDispatcher,
       %{
         state: 0,
         on_mpv: fn event, state ->
           send(parent, {:result, event, state})
           state + 1
         end
       }}
    )

    :ok
  end

  test "public API returns distinct references and forwards every command" do
    Process.register(self(), App.PlaybackOwner)
    command = ["get_property", "volume"]
    assert {:ok, first} = EasyTV.Controls.send_mpv_command(command)
    assert {:ok, second} = EasyTV.Controls.send_mpv_command(command)
    assert first != second
    assert_receive {:"$gen_cast", {:mpv_command, ^first, ^command}}
    assert_receive {:"$gen_cast", {:mpv_command, ^second, ^command}}
  end

  test "replies are correlated, delivered once, and update script state" do
    command = ["get_property", "volume"]
    ref = make_ref()
    state = %App.PlaybackOwner.State{current_player: self()}
    {:noreply, state} = App.PlaybackOwner.handle_cast({:mpv_command, ref, command}, state)
    assert_receive {:"$gen_cast", {:user_command, ^ref, ^command}}
    payload = %{tag: {:user, ref}, data: 50}
    message = {:mpv_event, self(), :reply, payload}
    {:noreply, state} = App.PlaybackOwner.handle_info(message, state)
    assert state.user_requests == %{}
    assert_receive {:result, %{ref: ^ref, result: {:ok, %{"data" => 50}}}, 0}
    {:noreply, _} = App.PlaybackOwner.handle_info(message, state)
    refute_receive {:result, _, _}

    other = make_ref()

    {:noreply, _} =
      App.PlaybackOwner.handle_cast(
        {:mpv_command, other, command},
        %App.PlaybackOwner.State{}
      )

    assert_receive {:result, %{ref: ^other, result: {:error, :no_player}}, 1}
  end

  test "timeout, command errors, replacement and player exit terminate pending requests" do
    capture_log(fn ->
      for failure <- [:timeout, :command_error, :replacement, :exit] do
        ref = make_ref()
        command = ["get_property", "volume"]

        {:noreply, state} =
          App.PlaybackOwner.handle_cast(
            {:mpv_command, ref, command},
            %App.PlaybackOwner.State{current_player: self()}
          )

        assert_receive {:"$gen_cast", {:user_command, ^ref, ^command}}

        {state, expected} =
          case failure do
            :replacement ->
              {App.PlaybackOwner.cancel_player_requests(state, self()), :player_replaced}

            :exit ->
              monitor = state.user_requests[ref].monitor

              {:noreply, state} =
                App.PlaybackOwner.handle_info(
                  {:DOWN, monitor, :process, self(), :normal},
                  state
                )

              {state, :player_exited}

            :timeout ->
              {:noreply, state} =
                App.PlaybackOwner.handle_info({:user_command_timeout, ref}, state)

              {state, :timeout}

            :command_error ->
              {:noreply, state} =
                App.PlaybackOwner.handle_info(
                  {:mpv_event, self(), :command_error,
                   %{tag: {:user, ref}, error: "property unavailable"}},
                  state
                )

              {state, "property unavailable"}
          end

        assert state.user_requests == %{}
        assert_receive {:result, %{ref: ^ref, result: {:error, ^expected}}, _}
      end
    end)
  end

  test "invalid commands and unavailable owner fail explicitly" do
    capture_log(fn ->
      assert {:error, :invalid_command} = EasyTV.Controls.send_mpv_command([])
      assert {:error, :invalid_command} = EasyTV.Controls.send_mpv_command(["test", self()])
      assert {:error, :not_running} = EasyTV.Controls.send_mpv_command(["get_property", "volume"])
    end)
  end

  test "a key callback stores a query reference and its reply sends a follow-up command" do
    stop_supervised!(EasyTV.ConfigDispatcher)
    Process.register(self(), App.PlaybackOwner)

    start_supervised!(
      {EasyTV.ConfigDispatcher,
       %{
         state: %{pending: nil},
         on_key: fn :query, state ->
           {:ok, ref} = EasyTV.Controls.send_mpv_command(["get_property", "volume"])
           %{state | pending: ref}
         end,
         on_mpv: fn
           %{ref: ref, result: {:ok, %{"data" => volume}}}, %{pending: ref} = state ->
             EasyTV.Controls.send_mpv_command(["set_property", "volume", volume + 5])
             %{state | pending: nil}
         end
       }}
    )

    send(EasyTV.ConfigDispatcher, {:input_event, :keyboard, [:query]})
    assert_receive {:"$gen_cast", {:mpv_command, ref, ["get_property", "volume"]}}

    EasyTV.ConfigDispatcher.mpv_event(%{
      type: :reply,
      ref: ref,
      result: {:ok, %{"error" => "success", "data" => 40}}
    })

    assert_receive {:"$gen_cast", {:mpv_command, _, ["set_property", "volume", 45]}}
    assert :sys.get_state(EasyTV.ConfigDispatcher).state == %{pending: nil}
  end

  test "mpv transport assigns wire IDs and preserves replies for user requests" do
    {:ok, listener} = :gen_tcp.listen(0, [:binary, active: false, ip: {127, 0, 0, 1}])
    {:ok, port} = :inet.port(listener)
    {:ok, socket} = :gen_tcp.connect({127, 0, 0, 1}, port, [:binary, active: false])
    {:ok, peer} = :gen_tcp.accept(listener)

    on_exit(fn ->
      Enum.each([socket, peer, listener], &:gen_tcp.close/1)
    end)

    ref = make_ref()
    command = ["get_property", "volume"]
    state = %MpvPlayer.State{parent: self(), socket: socket, buffer: "", stopping?: false}
    {:noreply, state} = MpvPlayer.handle_cast({:user_command, ref, command}, state)
    {:ok, wire} = :gen_tcp.recv(peer, 0, 1000)
    assert %{"request_id" => 1, "command" => ^command} = Jason.decode!(String.trim(wire))

    {:noreply, state} =
      MpvPlayer.handle_info(
        {:tcp, socket, ~s({"request_id":1,"error":"success","data":50}\n)},
        state
      )

    assert state.pending == %{}
    assert_receive {:mpv_event, _, :reply, %{tag: {:user, ^ref}, data: 50}}
  end
end
