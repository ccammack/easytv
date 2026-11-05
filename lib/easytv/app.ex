defmodule App do
  require Logger

  def main(argv) do
    {opts, mediadirs, invalid} =
      OptionParser.parse(argv,
        strict: [
          scan: :boolean,
          config: :string,
          cec_url: :string,
          state_dir: :string,
          temp_dir: :string,
          help: :boolean
        ],
        aliases: [h: :help]
      )

    cond do
      invalid != [] ->
        usage("invalid options: #{inspect(invalid)}")

      opts[:help] ->
        IO.puts(help())

      mediadirs == [] ->
        usage("missing media dirs")

      true ->
        Script.configure_paths!(opts)
        cec_url = if opts[:cec_url], do: EasyTV.Cecmond.validate_url!(opts[:cec_url])

        config =
          opts
          |> Keyword.get(:config, Script.hook_path("easytv.config.exs"))
          |> EasyTV.UserConfig.Loader.load!()

        mediadirs =
          mediadirs
          |> Enum.map(&Path.expand/1)
          |> Enum.uniq()

        scan? = Keyword.get(opts, :scan, false)

        saved_state = StateStore.load!()
        saved_channel_state = Map.fetch!(saved_state, "channel_state")
        saved_current_channel_id = Map.fetch!(saved_state, "current_channel_id")

        catalog = load_or_build_catalog!(mediadirs, scan?)
        channels = active_channels_for_roots(catalog, mediadirs)

        if channels == [] do
          usage("no playable media found in supplied media dirs")
        end

        current_channel =
          Enum.find(channels, fn channel ->
            App.Util.channel_id(channel) == saved_current_channel_id
          end) || List.first(channels)

        {:ok, session} =
          DynamicSupervisor.start_child(
            EasyTV.Supervisor,
            Supervisor.child_spec(
              {EasyTV.SessionSupervisor,
               %{
                 channels: channels,
                 current_channel: current_channel,
                 channel_state: saved_channel_state,
                 config: config,
                 cec_url: cec_url
               }},
              restart: :temporary
            )
          )

        ref = Process.monitor(session)

        receive do
          {:DOWN, ^ref, :process, ^session, :shutdown} ->
            if not EasyTV.Application.stopping?() do
              raise "EasyTV session stopped unexpectedly"
            end

          {:DOWN, ^ref, :process, ^session, reason} ->
            raise "EasyTV session exited: #{inspect(reason)}"
        end
    end
  end

  defp load_or_build_catalog!(mediadirs, true) do
    catalog =
      mediadirs
      |> Enum.reduce(%{}, fn rootdir, acc ->
        case DirectoryPlaylistLoader.scan_root(rootdir) do
          {:ok, channels} -> Map.put(acc, rootdir, %{"channels" => channels})
          {:error, _reason} -> acc
        end
      end)
      |> then(fn roots -> %{"version" => 1, "roots" => roots} end)

    :ok = CatalogStore.save!(catalog)
    catalog
  end

  defp load_or_build_catalog!(mediadirs, false) do
    catalog = CatalogStore.load!()
    roots = Map.get(catalog, "roots", %{})

    {updated_roots, changed?} =
      Enum.reduce(mediadirs, {roots, false}, fn rootdir, {acc, changed?} ->
        if Map.has_key?(acc, rootdir) do
          {acc, changed?}
        else
          case DirectoryPlaylistLoader.scan_root(rootdir) do
            {:ok, channels} -> {Map.put(acc, rootdir, %{"channels" => channels}), true}
            {:error, _reason} -> {acc, changed?}
          end
        end
      end)

    updated_catalog = %{"version" => 1, "roots" => updated_roots}
    if changed?, do: CatalogStore.save!(updated_catalog)
    updated_catalog
  end

  defp active_channels_for_roots(catalog, mediadirs) do
    roots = Map.get(catalog, "roots", %{})

    mediadirs
    |> Enum.flat_map(fn rootdir ->
      root_entry = Map.get(roots, rootdir, %{"channels" => %{}})
      channels = Map.get(root_entry, "channels", %{})

      channels
      |> Enum.map(fn {_channel_id, channeldata} ->
        %{
          root: rootdir,
          name: Map.fetch!(channeldata, "name"),
          path: Map.fetch!(channeldata, "path"),
          media: Map.fetch!(channeldata, "media"),
          label: "#{Map.fetch!(channeldata, "name")} (#{rootdir})"
        }
      end)
    end)
    |> Enum.sort_by(&{&1.root, &1.name, &1.path})
  end

  defp usage(message) do
    IO.puts(:stderr, message)
    IO.puts(:stderr, help())
    System.halt(1)
  end

  defp help do
    """
    usage: #{Script.name()} [--scan] MEDIA_DIR [MEDIA_DIR ...]

    --scan             Rebuild the catalog for supplied media roots.
    --config PATH      Load a user callback script (default: bundled priv/easytv.config.exs).
    --cec-url URL      Enable cecmond NDJSON observations and commands.
    --state-dir DIR    Catalog/playback state (default: .easytv/state).
    --temp-dir DIR     Prepared media and IPC sockets (default: .easytv/tmp).
    --help, -h         Show this help.

    Relative paths resolve from the working directory. Run development commands
    from the project root. Services should pass explicit writable storage paths.
    """
  end
end
