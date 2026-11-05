defmodule EasyTV.ProjectTest do
  use ExUnit.Case, async: true

  test "application identity and persistence filenames remain unchanged" do
    assert Script.name() == "easytv"
    assert Path.dirname(StateStore.state_path()) == Path.expand(".easytv/state")
    assert Path.basename(StateStore.state_path()) == "state.json"
    assert Path.basename(CatalogStore.catalog_path()) == "catalog.json"
  end

  test "packaged config resolves independently of the source file location" do
    path = Script.hook_path("easytv.config.exs")
    assert Path.dirname(path) == to_string(:code.priv_dir(:easytv))
    assert File.regular?(path)
  end

  test "application marks orderly shutdown before terminating session children" do
    code = """
    defmodule ShutdownProbe do
      use GenServer
      def start_link(parent), do: GenServer.start_link(__MODULE__, parent)
      def init(parent) do
        Process.flag(:trap_exit, true)
        {:ok, parent}
      end
      def terminate(_, parent) do
        send(parent, {:shutdown, EasyTV.Application.stopping?(),
                      Process.whereis(EasyTV.Supervisor) != nil})
      end
    end
    {:ok, _} = Application.ensure_all_started(:easytv)
    false = EasyTV.Application.stopping?()
    {:ok, _} = DynamicSupervisor.start_child(EasyTV.Supervisor, {ShutdownProbe, self()})
    :ok = Application.stop(:easytv)
    receive do
      {:shutdown, true, true} -> :ok
    after
      1000 -> raise "shutdown was not marked before child termination"
    end
    {:ok, _} = Application.ensure_all_started(:easytv)
    false = EasyTV.Application.stopping?()
    """

    {output, status} =
      System.cmd(System.find_executable("mix"), ["run", "--no-start", "-e", code],
        stderr_to_stdout: true
      )

    assert status == 0, output
  end

  @tag :tmp_dir
  test "Mix launcher preserves usage errors and does not require input hardware", %{
    tmp_dir: dir
  } do
    mix = System.find_executable("mix")

    for {args, message} <- [
          {[], "missing media dirs"},
          {["--invalid"], "invalid options:"},
          {["--scan", dir], "no playable media found in supplied media dirs"}
        ] do
      {output, status} =
        System.cmd(
          mix,
          [
            "easytv",
            "--state-dir",
            Path.join(dir, "state"),
            "--temp-dir",
            "/tmp/easytv-check" | args
          ],
          stderr_to_stdout: true
        )

      assert status == 1
      assert output =~ message
      assert output =~ "usage: easytv [--scan] MEDIA_DIR [MEDIA_DIR ...]"
    end
  end

  @tag :tmp_dir
  test "catalog scanning still finds nested media and excludes hidden entries", %{tmp_dir: dir} do
    File.mkdir_p!(Path.join(dir, "Movies/nested"))
    File.mkdir_p!(Path.join(dir, ".hidden"))
    File.write!(Path.join(dir, "Movies/nested/movie.mp4"), "")
    File.write!(Path.join(dir, "Movies/.hidden.mp4"), "")
    File.write!(Path.join(dir, ".hidden/movie.mp4"), "")

    assert {:ok, channels} = DirectoryPlaylistLoader.scan_root(dir)
    assert Map.keys(channels) == [Path.join(dir, "Movies")]

    assert channels[Path.join(dir, "Movies")]["media"] == [
             Path.join(dir, "Movies/nested/movie.mp4")
           ]
  end

  @tag :tmp_dir
  test "state and catalog JSON round-trip through the extracted modules", %{tmp_dir: dir} do
    state = %{
      "version" => 1,
      "current_channel_id" => "Movies",
      "channel_state" => %{"Movies" => %{"file_index" => 2, "offset_seconds" => 42}}
    }

    path = Path.join(dir, "state.json")
    assert :ok = JsonFileStore.save!(path, state, &StateStore.validate_state!/1)

    assert JsonFileStore.load!(
             path,
             StateStore.default_state(),
             &StateStore.validate_state!/1,
             "state file"
           ) == state

    catalog = %{"version" => 1, "roots" => %{dir => %{"channels" => %{}}}}
    path = Path.join(dir, "catalog.json")
    assert :ok = JsonFileStore.save!(path, catalog, &CatalogStore.validate_catalog!/1)

    assert JsonFileStore.load!(
             path,
             CatalogStore.default_catalog(),
             &CatalogStore.validate_catalog!/1,
             "catalog file"
           ) == catalog
  end
end
