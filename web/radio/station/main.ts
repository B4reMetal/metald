import { mount } from "svelte";
import Page from "../src/pages/Station.svelte";
import "../src/app.css";

mount(Page, { target: document.getElementById("app")! });
