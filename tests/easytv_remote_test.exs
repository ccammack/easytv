System.put_env("EASYTV_KEYBOARD_TEST", "1")
Code.require_file("../scripts/easytv-remote.exs", __DIR__)
ExUnit.start()

defmodule EasyTVRemoteTest do
  use ExUnit.Case
  import ExUnit.CaptureLog

  test "legacy channel mappings and audio stubs" do
    assert EasyTV.Keyboard.action(:key_pageup) == {"/channel", ~s({"direction":"previous"})}
    assert EasyTV.Keyboard.action(:key_pagedown) == {"/channel", ~s({"direction":"next"})}
    assert EasyTV.Keyboard.channel_prev() == {"/channel", ~s({"direction":"previous"})}
    assert EasyTV.Keyboard.channel_next() == {"/channel", ~s({"direction":"next"})}

    for key <- [:key_equal, :key_plus, :key_kpplus],
        do: assert(EasyTV.Keyboard.action(key) == :ignore)

    for key <- [:key_minus, :key_kpminus],
        do: assert(EasyTV.Keyboard.action(key) == :ignore)

    assert EasyTV.Keyboard.action(:key_m) == :ignore
    assert EasyTV.Keyboard.volume_up() == :ignore
    assert EasyTV.Keyboard.volume_down() == :ignore
    assert EasyTV.Keyboard.mute() == :ignore
    assert EasyTV.Keyboard.action(:key_space) == :ignore
    assert EasyTV.Keyboard.power() == :ignore
    assert EasyTV.Keyboard.action(:key_q) == :ignore
  end

  test "test mode numbers channel key-downs and logs repeats without REST" do
    parent = self()

    output =
      capture_log(fn ->
        assert {2, :second} =
                 EasyTV.Keyboard.test_events(
                   [
                     {:ev_key, :key_pageup, 1},
                     {:ev_key, :key_pageup, 2},
                     {:ev_key, :key_pageup, 0},
                     {:ev_key, :key_space, 1},
                     {:ev_key, :key_pagedown, 1}
                   ],
                   0,
                   nil,
                   fn text, previous ->
                     send(parent, {:overlay, text, previous})
                     if previous, do: :second, else: :first
                   end
                 )
      end)

    assert_receive {:overlay, "REMOTE TEST 1: key_pageup", nil}
    assert_receive {:overlay, "REMOTE TEST 2: key_pagedown", :first}
    refute_receive {:overlay, _, _}
    assert output =~ "TEST key=key_pageup value=2"
    assert output =~ "TEST key=key_pageup value=0"
    refute output =~ "dispatch"
  end

  test "release, repeat, power/audio stubs and unrelated keys do not call HTTP" do
    assert capture_log(fn ->
             EasyTV.Keyboard.handle(
               [
                 {:ev_key, :key_pageup, 0},
                 {:ev_key, :key_pageup, 2},
                 {:ev_key, :key_space, 1},
                 {:ev_key, :key_equal, 1},
                 {:ev_key, :key_plus, 1},
                 {:ev_key, :key_kpplus, 1},
                 {:ev_key, :key_minus, 1},
                 {:ev_key, :key_kpminus, 1},
                 {:ev_key, :key_m, 1},
                 {:ev_key, :key_q, 1},
                 {:ev_syn, :syn_report, 0}
               ],
               "not-a-url"
             )
           end) == ""
  end

  test "HTTP response and errors are reported" do
    {:ok, _} = Application.ensure_all_started(:inets)
    {:ok, listener} = :gen_tcp.listen(0, [:binary, active: false, reuseaddr: true])
    {:ok, port} = :inet.port(listener)
    parent = self()

    server =
      Task.async(fn ->
        {:ok, socket} = :gen_tcp.accept(listener, 3000)
        {:ok, data} = :gen_tcp.recv(socket, 0, 3000)
        send(parent, {:request, data})
        :ok = :gen_tcp.send(socket, "HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")
        :gen_tcp.close(socket)
      end)

    assert EasyTV.Keyboard.post("http://127.0.0.1:#{port}", "/channel", ~s({"direction":"next"})) ==
             :ok

    assert_receive {:request, data}
    assert data =~ "POST /channel"
    Task.await(server)
    :gen_tcp.close(listener)
    assert {:error, _} = EasyTV.Keyboard.post("http://127.0.0.1:#{port}", "/channel", "{}")
  end

  test "successive key-downs dispatch in order without repeats" do
    {:ok, _} = Application.ensure_all_started(:inets)
    {:ok, listener} = :gen_tcp.listen(0, [:binary, active: false, reuseaddr: true])
    on_exit(fn -> :gen_tcp.close(listener) end)
    {:ok, port} = :inet.port(listener)
    parent = self()

    server =
      Task.async(fn ->
        for _ <- 1..3 do
          {:ok, socket} = :gen_tcp.accept(listener, 3000)
          {:ok, data} = :gen_tcp.recv(socket, 0, 3000)
          send(parent, {:request, data})
          :ok = :gen_tcp.send(socket, "HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")
          :gen_tcp.close(socket)
        end
      end)

    output =
      capture_log([level: :debug], fn ->
        EasyTV.Keyboard.handle(
          [
            {:ev_key, :key_pageup, 1},
            {:ev_key, :key_pageup, 0},
            {:ev_key, :key_pagedown, 1},
            {:ev_key, :key_pagedown, 2},
            {:ev_key, :key_pagedown, 0},
            {:ev_key, :key_pageup, 1}
          ],
          "http://127.0.0.1:#{port}"
        )
      end)

    for direction <- ["previous", "next", "previous"] do
      assert_receive {:request, data}
      assert data =~ ~s({"direction":"#{direction}"})
    end

    Task.await(server)
    assert length(Regex.scan(~r/: dispatch/, output)) == 3
    assert length(Regex.scan(~r/: accepted elapsed_ms=\d+/, output)) == 3
  end

  test "intentional overlay termination is quiet but failures are reported" do
    assert capture_log(fn ->
             EasyTV.Keyboard.report_overlay_exit(0)
             EasyTV.Keyboard.report_overlay_exit(143)
           end) == ""

    assert capture_log(fn -> EasyTV.Keyboard.report_overlay_exit(23) end) =~
             "OSD exited status=23"
  end
end
