defmodule Mix.Tasks.Easytv do
  use Mix.Task

  @shortdoc "Runs EasyTV with the supplied media directories"

  @impl true
  def run(args) do
    Mix.Tasks.Run.run(["-e", "EasyTV.CLI.main(System.argv())", "--" | args])
  end
end
