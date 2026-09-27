export namespace main {
	
	export class ClientState {
	    running: boolean;
	    host: string;
	    port: number;
	    serverURL: string;
	    localDir: string;
	    interval: number;
	    lastSync: string;
	    count: number;
	    online: boolean;
	    error: string;
	    configPath: string;
	
	    static createFrom(source: any = {}) {
	        return new ClientState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.serverURL = source["serverURL"];
	        this.localDir = source["localDir"];
	        this.interval = source["interval"];
	        this.lastSync = source["lastSync"];
	        this.count = source["count"];
	        this.online = source["online"];
	        this.error = source["error"];
	        this.configPath = source["configPath"];
	    }
	}
	export class FileItem {
	    name: string;
	    size: number;
	    modified: string;
	
	    static createFrom(source: any = {}) {
	        return new FileItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.size = source["size"];
	        this.modified = source["modified"];
	    }
	}

}

