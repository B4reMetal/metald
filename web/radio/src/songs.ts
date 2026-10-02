import { mount } from "svelte";
import Page from "./pages/Songs.svelte";
import "./app.css";

mount(Page, { target: document.getElementById("app")! });
