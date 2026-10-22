"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
const node_fs_1 = __importDefault(require("node:fs"));
const core_1 = require("@electron-forge/core");
const prompts_1 = require("@inquirer/prompts");
const prompt_adapter_inquirer_1 = require("@listr2/prompt-adapter-inquirer");
const chalk_1 = __importDefault(require("chalk"));
const commander_1 = require("commander");
const listr2_1 = require("listr2");
require("./util/terminate");
const package_json_1 = __importDefault(require("../package.json"));
const resolve_working_dir_1 = require("./util/resolve-working-dir");
commander_1.program
    .version(package_json_1.default.version, '-V, --version', 'Output the current version.')
    .helpOption('-h, --help', 'Output usage information.')
    .argument('[dir]', 'Directory to initialize the project in. Defaults to the current directory.')
    .option('-t, --template [name]', 'Name of the Forge template to use.')
    .option('-c, --copy-ci-files', 'Whether to copy the templated CI files.')
    .option('-f, --force', 'Whether to overwrite an existing directory.')
    .option('--skip-git', 'Skip initializing a git repository in the initialized project.')
    .option('--electron-version [version]', 'Set a specific Electron version for your Forge project. Can take in a version string (e.g. `38.3.0`) or `latest`, `beta`, or `nightly` tags.')
    .action(async (dir) => {
    const options = commander_1.program.opts();
    const tasks = new listr2_1.Listr([
        {
            task: async (initOpts) => {
                initOpts.interactive = true;
                initOpts.template = options.template ?? 'base';
                initOpts.copyCIFiles = Boolean(options.copyCiFiles);
                initOpts.force = Boolean(options.force);
                initOpts.skipGit = Boolean(options.skipGit);
                initOpts.dir = (0, resolve_working_dir_1.resolveWorkingDir)(dir, false);
                initOpts.electronVersion = options.electronVersion ?? 'latest';
            },
        },
        {
            task: async (initOpts, task) => {
                // only run interactive prompts if no args passed and not in CI environment
                if (Object.keys(options).length > 0 ||
                    process.env.CI ||
                    !process.stdout.isTTY) {
                    return;
                }
                const prompt = task.prompt(prompt_adapter_inquirer_1.ListrInquirerPromptAdapter);
                if (typeof initOpts.dir === 'string' &&
                    node_fs_1.default.existsSync(initOpts.dir) &&
                    (await node_fs_1.default.promises.readdir(initOpts.dir)).length > 0) {
                    const confirmResult = await prompt.run(prompts_1.confirm, {
                        message: `${chalk_1.default.cyan(initOpts.dir)} is not empty. Would you like to continue and overwrite existing files?`,
                        default: false,
                    });
                    if (confirmResult) {
                        initOpts.force = true;
                    }
                    else {
                        task.output = 'Directory is not empty. Exiting.';
                        process.exit(0);
                    }
                }
                const bundler = await prompt.run(prompts_1.select, {
                    message: 'Select a bundler',
                    choices: [
                        {
                            name: 'None',
                            value: 'base',
                        },
                        {
                            name: 'Vite (Experimental)',
                            value: 'vite',
                        },
                        {
                            name: 'webpack',
                            value: 'webpack',
                        },
                    ],
                });
                let language;
                if (bundler !== 'base') {
                    language = await prompt.run(prompts_1.select, {
                        message: 'Select a programming language',
                        choices: [
                            {
                                name: 'JavaScript',
                                value: undefined,
                            },
                            {
                                name: 'TypeScript',
                                value: 'typescript',
                            },
                        ],
                    });
                }
                initOpts.template = `${bundler}${language ? `-${language}` : ''}`;
                // TODO: add prompt for passing in an exact version as well
                initOpts.electronVersion = await prompt.run(prompts_1.select, {
                    message: 'Select an Electron release',
                    choices: [
                        {
                            name: 'electron@latest',
                            value: 'latest',
                        },
                        {
                            name: 'electron@beta',
                            value: 'beta',
                        },
                        {
                            name: 'electron-nightly@latest',
                            value: 'nightly',
                        },
                    ],
                });
                initOpts.skipGit = !(await prompt.run(prompts_1.confirm, {
                    message: `Would you like to initialize Git in your new project?`,
                    default: true,
                }));
            },
        },
    ], { concurrent: false });
    const initOpts = await tasks.run();
    await core_1.api.init(initOpts);
});
commander_1.program.parse(process.argv);
//# sourceMappingURL=data:application/json;base64,eyJ2ZXJzaW9uIjozLCJmaWxlIjoiZWxlY3Ryb24tZm9yZ2UtaW5pdC5qcyIsInNvdXJjZVJvb3QiOiIiLCJzb3VyY2VzIjpbIi4uL3NyYy9lbGVjdHJvbi1mb3JnZS1pbml0LnRzIl0sIm5hbWVzIjpbXSwibWFwcGluZ3MiOiI7Ozs7O0FBQUEsc0RBQXlCO0FBRXpCLCtDQUF3RDtBQUN4RCwrQ0FBb0Q7QUFDcEQsNkVBQTZFO0FBQzdFLGtEQUEwQjtBQUMxQix5Q0FBb0M7QUFDcEMsbUNBQStCO0FBRS9CLDRCQUEwQjtBQUMxQixtRUFBMEM7QUFFMUMsb0VBQStEO0FBSy9ELG1CQUFPO0tBQ0osT0FBTyxDQUFDLHNCQUFXLENBQUMsT0FBTyxFQUFFLGVBQWUsRUFBRSw2QkFBNkIsQ0FBQztLQUM1RSxVQUFVLENBQUMsWUFBWSxFQUFFLDJCQUEyQixDQUFDO0tBQ3JELFFBQVEsQ0FDUCxPQUFPLEVBQ1AsNEVBQTRFLENBQzdFO0tBQ0EsTUFBTSxDQUFDLHVCQUF1QixFQUFFLG9DQUFvQyxDQUFDO0tBQ3JFLE1BQU0sQ0FBQyxxQkFBcUIsRUFBRSx5Q0FBeUMsQ0FBQztLQUN4RSxNQUFNLENBQUMsYUFBYSxFQUFFLDZDQUE2QyxDQUFDO0tBQ3BFLE1BQU0sQ0FDTCxZQUFZLEVBQ1osZ0VBQWdFLENBQ2pFO0tBQ0EsTUFBTSxDQUNMLDhCQUE4QixFQUM5Qiw4SUFBOEksQ0FDL0k7S0FDQSxNQUFNLENBQUMsS0FBSyxFQUFFLEdBQUcsRUFBRSxFQUFFO0lBQ3BCLE1BQU0sT0FBTyxHQUFHLG1CQUFPLENBQUMsSUFBSSxFQUFFLENBQUM7SUFDL0IsTUFBTSxLQUFLLEdBQUcsSUFBSSxjQUFLLENBQ3JCO1FBQ0U7WUFDRSxJQUFJLEVBQUUsS0FBSyxFQUFFLFFBQVEsRUFBaUIsRUFBRTtnQkFDdEMsUUFBUSxDQUFDLFdBQVcsR0FBRyxJQUFJLENBQUM7Z0JBQzVCLFFBQVEsQ0FBQyxRQUFRLEdBQUcsT0FBTyxDQUFDLFFBQVEsSUFBSSxNQUFNLENBQUM7Z0JBQy9DLFFBQVEsQ0FBQyxXQUFXLEdBQUcsT0FB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