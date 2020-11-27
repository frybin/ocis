import {defaults, playbook} from '../../lib'
import {Options} from 'k6/options';
import {sleep} from "k6";
import * as auth from "../../lib/auth";
import * as types from "../../lib/types";

interface dataI {
    credential: types.Account | types.Token;
}

export const options: Options = {
    ...defaults.k6OptionsDefault,
    iterations: 1,
    vus: 1,
};
const account = defaults.knownAccounts.einstein;
const playbooks = {
    fileUpload: playbook.dav.fileUpload(),
    fileDownload: playbook.dav.fileDownload(),
    fileDelete: playbook.dav.fileDelete(),
}
export const setup = (): dataI => {
    return {
        credential: defaults.OC_OIDC ? auth.oidc(account) : account,
    }
}
export default (data: dataI) => {
    const credential = data.credential;
    const userName = account.login;
    const fileName = playbooks.fileUpload({
        credential,
        userName,
        asset: defaults.OC_TEST_FILE
    });

    sleep(1)

    playbooks.fileDownload({
        credential,
        userName,
        fileName,
    });

    sleep(1)

    playbooks.fileDelete({
        credential,
        userName,
        fileName,
    });

    sleep(1)
};