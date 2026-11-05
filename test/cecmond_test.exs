defmodule EasyTV.CecmondTest do
  use ExUnit.Case
  import ExUnit.CaptureLog

  test "commands are forwarded independently without coalescing or changing payloads" do
    start_supervised!({Task.Supervisor, name: App.TaskSupervisor})

    {:ok, listener} =
      :gen_tcp.listen(0, [:binary, active: false, reuseaddr: true, ip: {127, 0, 0, 1}])

    on_exit(fn -> :gen_tcp.close(listener) end)
    {:ok, port} = :inet.port(listener)
    start_supervised!({EasyTV.Cecmond, "http://127.0.0.1:#{port}"})
    parent = self()

    server =
      Task.async(fn ->
        sockets =
          for _ <- 1..3 do
            {:ok, socket} = :gen_tcp.accept(listener, 2000)
            {headers, body} = read_request(socket, "")
            send(parent, {:request, headers, Jason.decode!(body)})
            socket
          end

        receive do
          :reply -> :ok
        after
          2000 -> raise "missing response signal"
        end

        Enum.each(sockets, fn socket ->
          :ok =
            :gen_tcp.send(socket, "HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")

          :gen_tcp.close(socket)
        end)
      end)

    command = %{command: "active_source"}
    other = %{command: "test_command", destination: 0, parameters: [1, 2]}
    assert {:ok, first} = EasyTV.Controls.send_cec_command(command)
    assert {:ok, second} = EasyTV.Controls.send_cec_command(command)
    assert {:ok, third} = EasyTV.Controls.send_cec_command(other)
    assert Enum.all?([first, second, third], &is_reference/1)
    assert length(Enum.uniq([first, second, third])) == 3

    requests =
      for _ <- 1..3 do
        assert_receive {:request, headers, body}, 3000
        assert headers =~ "POST /commands HTTP/1.1"
        body
      end

    assert Enum.count(requests, &(&1 == %{"command" => "active_source"})) == 2
    assert %{"command" => "test_command", "destination" => 0, "parameters" => [1, 2]} in requests
    send(server.pid, :reply)
    Task.await(server)
  end

  test "invalid payloads and disabled CEC report errors" do
    assert capture_log(fn ->
             assert {:error, :invalid_command} = EasyTV.Controls.send_cec_command("invalid")
             assert {:error, :invalid_command} = EasyTV.Controls.send_cec_command(%{bad: self()})

             assert {:error, :not_running} =
                      EasyTV.Controls.send_cec_command(%{command: "active_source"})
           end) =~ "CEC command"
  end

  defp read_request(socket, data) do
    case String.split(data, "\r\n\r\n", parts: 2) do
      [headers, body] ->
        [_, length] = Regex.run(~r/content-length:\s*(\d+)/i, headers)

        if byte_size(body) >= String.to_integer(length) do
          {headers, body}
        else
          {:ok, chunk} = :gen_tcp.recv(socket, 0, 2000)
          read_request(socket, data <> chunk)
        end

      [_] ->
        {:ok, chunk} = :gen_tcp.recv(socket, 0, 2000)
        read_request(socket, data <> chunk)
    end
  end
end
