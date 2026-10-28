System.put_env("EASYTV_KEYBOARD_TEST", "1")
Code.require_file("../scripts/easytv-remote.exs", __DIR__)
ExUnit.start()

defmodule KeyboardTest do
  use ExUnit.Case
  import ExUnit.CaptureLog

  test "legacy channel mappings and audio stubs" do
    assert EasyTV.Keyboard.action(:key_pageup) == {"/channel", ~s({"direction":"previous"})}
    assert EasyTV.Keyboard.action(:key_pagedown) == {"/channel", ~s({"direction":"next"})}

    for key <- [:key_equal, :key_plus, :key_kpplus],
        do: assert(EasyTV.Keyboard.action(key) == :volume_up)

    for key <- [:key_minus, :key_kpminus],
        do: assert(EasyTV.Keyboard.action(key) == :volume_down)

    assert EasyTV.Keyboard.action(:key_m) == :mute
    assert EasyTV.Keyboard.volume_up() == :ok
    assert EasyTV.Keyboard.volume_down() == :ok
    assert EasyTV.Keyboard.mute() == :ok
    assert EasyTV.Keyboard.action(:key_space) == :power
    assert EasyTV.Keyboard.power() == :ok
    assert EasyTV.Keyboard.action(:key_q) == :ignore
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
end
