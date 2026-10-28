#!/usr/bin/env elixir

defmodule EasyTV.Keyboard do
  require Logger

  def action(:key_pageup), do: channel_prev()
  def action(:key_pagedown), do: channel_next()
  def action(:key_m), do: mute()
  def action(key) when key in [:key_minus, :key_kpminus], do: volume_down()
  def action(key) when key in [:key_equal, :key_plus, :key_kpplus], do: volume_up()
  def action(:key_space), do: power()
  def action(_), do: :ignore

  def channel_prev, do: {"/channel", ~s({"direction":"previous"})}
  def channel_next, do: {"/channel", ~s({"direction":"next"})}

  # Stubs: the remote controls TV power, volume and mute directly.
  def power, do: :ignore
  def volume_up, do: :ignore
  def volume_down, do: :ignore
  def mute, do: :ignore

  def post(base, path, body) do
    request = {String.to_charlist(base <> path), [], ~c"application/json", body}

    case :httpc.request(:post, request, [timeout: 5000, connect_timeout: 2000], []) do
      {:ok, {{_, 204, _}, _, _}} ->
        :ok

      {:ok, {{_, code, _}, _, response}} ->
        {:error, "HTTP #{code}: #{IO.iodata_to_binary(response)}"}

      {:error, reason} ->
        {:error, inspect(reason)}
    end
  end

  def handle(events, base) do
    Enum.each(events, fn
      {:ev_key, key, 1} ->
        case action(key) do
          :ignore ->
            :ok

          {path, body} ->
            started = System.monotonic_time(:millisecond)
            Logger.debug("key=#{key} #{path}: dispatch")

            case post(base, path, body) do
              :ok ->
                Logger.debug(
                  "key=#{key} #{path}: accepted elapsed_ms=#{System.monotonic_time(:millisecond) - started}"
                )

              {:error, reason} ->
                Logger.error(
                  "key=#{key} #{path}: #{reason} elapsed_ms=#{System.monotonic_time(:millisecond) - started}"
                )
            end
        end

      _ ->
        :ok
    end)
  end

  def test_events(events, count, overlay, show) do
    Enum.reduce(events, {count, overlay}, fn
      {:ev_key, key, value}, {count, overlay} when key in [:key_pageup, :key_pagedown] ->
        Logger.info("TEST key=#{key} value=#{value}")

        if value == 1 do
          count = count + 1
          {count, show.("REMOTE TEST #{count}: #{key}", overlay)}
        else
          {count, overlay}
        end

      _, state ->
        state
    end)
  end

  defp test_overlay(executable, text) do
    port =
      Port.open({:spawn_executable, executable}, [
        :binary,
        :use_stdio,
        :exit_status,
        :stderr_to_stdout,
        args: [
          "--position",
          "0",
          "--x-offset",
          "32",
          "--y-offset",
          "32",
          "--padding",
          "16",
          "--transparency",
          "0",
          "--font",
          "monospace 24",
          "--back-color",
          "black",
          "--fore-color",
          "white",
          "--fade-in",
          "0",
          "--fade-full",
          "5000",
          "--fade-out",
          "0",
          "--lines",
          "1"
        ]
      ])

    true = Port.command(port, text <> "\n")
    port
  end

  defp close_test_overlay(port) do
    case Port.info(port, :os_pid) do
      {:os_pid, pid} ->
        case System.cmd("kill", ["-TERM", Integer.to_string(pid)], stderr_to_stdout: true) do
          {_, 0} ->
            :ok

          {output, status} ->
            Logger.error("OSD terminate status=#{status}: #{String.trim(output)}")
        end

        if Port.info(port), do: Port.close(port)

      nil ->
        :ok
    end
  end

  def loop(base, test \\ nil, count \\ 0, overlay \\ nil) do
    receive do
      {:input_event, _device, events} when is_list(events) ->
        if test do
          {count, overlay} =
            test_events(events, count, overlay, fn text, previous ->
              if previous, do: close_test_overlay(previous)
              test_overlay(test, text)
            end)

          loop(base, test, count, overlay)
        else
          handle(events, base)
          loop(base)
        end

      {port, {:data, data}} when is_port(port) ->
        Logger.warning("OSD output: #{String.trim(data)}")
        loop(base, test, count, overlay)

      {port, {:exit_status, status}} when is_port(port) ->
        report_overlay_exit(status)
        loop(base, test, count, if(port == overlay, do: nil, else: overlay))

      message ->
        Logger.warning("unexpected keyboard message: #{inspect(message)}")
        loop(base, test, count, overlay)
    end
  end

  def report_overlay_exit(status) when status in [0, 143], do: :ok
  def report_overlay_exit(status), do: Logger.error("OSD exited status=#{status}")

  def main(args) do
    {opts, devices, invalid} =
      OptionParser.parse(args,
        strict: [url: :string, help: :boolean, prepare: :boolean, test: :boolean]
      )

    cond do
      invalid != [] ->
        raise ArgumentError, "invalid options: #{inspect(invalid)}"

      opts[:help] ->
        IO.puts("""
        Usage: easytv-remote.exs [--url http://127.0.0.1:8080] [DEVICE ...]
        --prepare: install/cache dependencies without opening input devices.
        --test: grab input and show numbered aosd_cat feedback; never call REST.
        Test mode requires DISPLAY/XAUTHORITY and aosd_cat. Exit releases input.
        Defaults to /dev/input/by-id/*-event-kbd; explicit devices are recommended.
        PgUp/PgDn: previous/next channel.
        Space, +/-, M: power/volume/mute stubs (remote controls TV directly). Q: ignored.
        Only key-down events are handled; repeats/releases are ignored.
        Requires read access to selected input devices. Ctrl-C stops this script.
        """)

      opts[:prepare] ->
        Mix.install([{:input_event, "~> 1.4"}])

      true ->
        base = String.trim_trailing(Keyword.get(opts, :url, "http://127.0.0.1:8080"), "/")
        uri = URI.parse(base)

        if uri.scheme not in ["http", "https"] or is_nil(uri.host),
          do: raise(ArgumentError, "--url must be an HTTP(S) URL")

        paths = if devices == [], do: Path.wildcard("/dev/input/by-id/*-event-kbd"), else: devices
        paths = paths |> Enum.map(&Path.expand/1) |> Enum.uniq()

        if paths == [],
          do: raise("No keyboard devices found; supply /dev/input/eventN explicitly")

        Mix.install([{:input_event, "~> 1.4"}])

        test =
          if opts[:test] do
            System.find_executable("aosd_cat") || raise("Test mode requires aosd_cat")
          end

        unless test do
          {:ok, _} = Application.ensure_all_started(:inets)
          {:ok, _} = Application.ensure_all_started(:ssl)
        end

        receiver = self()

        children =
          Enum.map(paths, fn path ->
            %{
              id: path,
              start: {InputEvent, :start_link, [[path: path, receiver: receiver, grab: !!test]]}
            }
          end)

        {:ok, _supervisor} = Supervisor.start_link(children, strategy: :one_for_one)

        if test do
          Logger.info(
            "Keyboard TEST: direct OSD, no REST; exclusive input devices=#{inspect(paths)}"
          )
        else
          Logger.info("Keyboard REST target=#{base} devices=#{inspect(paths)}")
        end

        loop(base, test)
    end
  end
end

unless System.get_env("EASYTV_KEYBOARD_TEST") == "1" do
  EasyTV.Keyboard.main(System.argv())
end
