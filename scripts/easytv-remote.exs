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
            case post(base, path, body) do
              :ok -> :ok
              {:error, reason} -> Logger.error("key=#{key} #{path}: #{reason}")
            end
        end

      _ ->
        :ok
    end)
  end

  def loop(base) do
    receive do
      {:input_event, _device, events} when is_list(events) ->
        handle(events, base)
        loop(base)

      message ->
        Logger.warning("unexpected keyboard message: #{inspect(message)}")
        loop(base)
    end
  end

  def main(args) do
    {opts, devices, invalid} =
      OptionParser.parse(args, strict: [url: :string, help: :boolean, prepare: :boolean])

    cond do
      invalid != [] ->
        raise ArgumentError, "invalid options: #{inspect(invalid)}"

      opts[:help] ->
        IO.puts("""
        Usage: easytv-remote.exs [--url http://127.0.0.1:8080] [DEVICE ...]
        --prepare: install/cache dependencies without opening input devices.
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
        {:ok, _} = Application.ensure_all_started(:inets)
        {:ok, _} = Application.ensure_all_started(:ssl)
        receiver = self()

        children =
          Enum.map(paths, fn path ->
            %{id: path, start: {InputEvent, :start_link, [[path: path, receiver: receiver]]}}
          end)

        {:ok, _supervisor} = Supervisor.start_link(children, strategy: :one_for_one)
        Logger.info("Keyboard REST target=#{base} devices=#{inspect(paths)}")
        loop(base)
    end
  end
end

unless System.get_env("EASYTV_KEYBOARD_TEST") == "1" do
  EasyTV.Keyboard.main(System.argv())
end
