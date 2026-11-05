defmodule EasyTV.MixProject do
  use Mix.Project

  def project do
    [
      app: :easytv,
      version: "0.1.0",
      elixir: "~> 1.14",
      start_permanent: Mix.env() == :prod,
      deps: [
        {:input_event, "~> 1.4"},
        {:jason, "~> 1.4"}
      ],
      releases: [
        easytv: [include_erts: true, overlays: ["rel/overlays"]]
      ]
    ]
  end

  def application do
    [mod: {EasyTV.Application, []}, extra_applications: [:logger, :inets, :ssl]]
  end
end
